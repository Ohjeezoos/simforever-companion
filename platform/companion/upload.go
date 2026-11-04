package companion

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Ohjeezoos/simforever-companion/platform/probe"
)

// Result is the platform's answer to a probe upload: what it stored and what
// it measured.
type Result struct {
	UploadID       int64   `json:"uploadId"`
	Build          string  `json:"build"`
	MeasurementIDs []int32 `json:"measurementIds"`
	probe.Summary
}

type envelope struct {
	Data  Result `json:"data"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// HTTPError is a refusal from the platform. Permanent ones are reported and
// never retried, because retrying a rejected export only wastes the player's
// bandwidth.
type HTTPError struct {
	Status     int
	Code       string
	Message    string
	retryAfter time.Duration
}

func (e *HTTPError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("server returned %d", e.Status)
	}
	return fmt.Sprintf("server returned %d %s: %s", e.Status, e.Code, e.Message)
}

// Permanent is true for 4xx other than the two the server uses to mean "later".
func (e *HTTPError) Permanent() bool {
	return e.Status >= 400 && e.Status < 500 && e.Status != http.StatusRequestTimeout && e.Status != http.StatusTooManyRequests
}

// RetryAfter is the wait the server asked for, or zero.
func (e *HTTPError) RetryAfter() time.Duration { return e.retryAfter }

type permanent interface{ Permanent() bool }

// IsPermanent reports whether retrying this error can ever help.
func IsPermanent(err error) bool {
	var p permanent
	return errors.As(err, &p) && p.Permanent()
}

type Client struct {
	BaseURL     string
	Game        string
	DisplayName string
	HTTP        *http.Client
}

func NewClient(cfg *Config) *Client {
	return &Client{BaseURL: cfg.APIBase, Game: cfg.Game, DisplayName: cfg.DisplayName,
		HTTP: &http.Client{Timeout: 2 * time.Minute}}
}

// URL is the probe route this client posts to.
func (c *Client) URL() string {
	return strings.TrimRight(c.BaseURL, "/") + "/v1/games/" + c.Game + "/probe"
}

// Upload posts one payload. It makes exactly one attempt; retrying is the
// caller's Policy.
func (c *Client) Upload(ctx context.Context, payload []byte) (*Result, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL(), bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "simforever-companion/"+Version)
	if c.DisplayName != "" {
		req.Header.Set("X-Simforever-Display-Name", c.DisplayName)
	}
	client := c.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	var env envelope
	jsonErr := json.Unmarshal(body, &env)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		e := &HTTPError{Status: resp.StatusCode}
		if jsonErr == nil && env.Error != nil {
			e.Code, e.Message = env.Error.Code, env.Error.Message
		} else {
			e.Message = strings.TrimSpace(firstLine(string(body)))
		}
		if d, err := time.ParseDuration(resp.Header.Get("Retry-After") + "s"); err == nil && d > 0 {
			e.retryAfter = d
		}
		return nil, e
	}
	if jsonErr != nil {
		return nil, fmt.Errorf("the server's answer was not JSON: %w", jsonErr)
	}
	return &env.Data, nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 200 {
		s = s[:200] + "..."
	}
	return s
}

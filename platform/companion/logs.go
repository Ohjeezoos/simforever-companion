package companion

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Upload the game-written combat log after it stops growing.

// LogStableFor is how long a combat log must go unchanged before it is sent.
const LogStableFor = 60 * time.Second

// LogResult is the platform's answer to a combat log upload.
type LogResult struct {
	UploadID  int64  `json:"uploadId"`
	JobID     int64  `json:"jobId"`
	SHA256    string `json:"sha256"`
	Duplicate bool   `json:"duplicate"`
	SizeBytes int64  `json:"sizeBytes"`
}

type logEnvelope struct {
	Data  LogResult `json:"data"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// LogURL is the combat log route this client posts to.
func (c *Client) LogURL() string {
	return strings.TrimRight(c.BaseURL, "/") + "/v1/games/" + c.Game + "/logs"
}

// UploadLog posts one combat log file. One attempt; retrying is the caller's.
func (c *Client) UploadLog(ctx context.Context, path string) (*LogResult, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	// The file is streamed into the request rather than held in memory: a long
	// raid night's log is hundreds of megabytes.
	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go func() {
		part, err := mw.CreateFormFile("file", filepath.Base(path))
		if err == nil {
			_, err = io.Copy(part, f)
		}
		if err != nil {
			pw.CloseWithError(err)
			return
		}
		pw.CloseWithError(mw.Close())
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.LogURL(), pr)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
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
	var env logEnvelope
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

// HashFile is the content hash of a file, which is what the platform dedupes
// on, so a log that has not grown since the last send is not sent again.
func HashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// looksLikeCombatLog reads the first line and checks it starts the way every
// combat log line does, so a file that merely shares the name is left alone.
func looksLikeCombatLog(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	head := make([]byte, 512)
	n, _ := io.ReadFull(f, head)
	if n == 0 {
		return false
	}
	line := head[:n]
	if i := bytes.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	// "M/D HH:MM:SS.mmm  EVENT,..." or "M/D/YYYY HH:MM:SS.mmm-Z  EVENT,..."
	i := bytes.Index(line, []byte("  "))
	if i <= 0 {
		return false
	}
	stamp := string(bytes.TrimPrefix(line[:i], []byte("\xef\xbb\xbf")))
	return strings.Count(stamp, "/") >= 1 && strings.Count(stamp, ":") >= 2
}

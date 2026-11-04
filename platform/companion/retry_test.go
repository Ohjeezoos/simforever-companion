package companion

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestPolicyWaitDoublesAndCaps(t *testing.T) {
	p := Policy{Base: time.Second, Max: 8 * time.Second}
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 8 * time.Second}
	for i, w := range want {
		if got := p.Wait(i + 1); got != w {
			t.Errorf("Wait(%d) = %s, want %s", i+1, got, w)
		}
	}
	d := Policy{}
	if d.Wait(1) != 5*time.Second || d.Wait(20) != 5*time.Minute {
		t.Fatalf("defaults wrong: %s %s", d.Wait(1), d.Wait(20))
	}
}

func TestPolicyDoRetriesTransientAndStopsOnPermanent(t *testing.T) {
	p := Policy{Base: time.Millisecond, Max: time.Millisecond}
	tries := 0
	err := p.Do(context.Background(), func(int) error {
		tries++
		if tries < 3 {
			return &HTTPError{Status: http.StatusInternalServerError}
		}
		return nil
	})
	if err != nil || tries != 3 {
		t.Fatalf("tries=%d err=%v", tries, err)
	}

	tries = 0
	err = p.Do(context.Background(), func(int) error {
		tries++
		return &HTTPError{Status: http.StatusUnprocessableEntity, Code: "invalid_export"}
	})
	if tries != 1 {
		t.Fatalf("a 4xx must not be retried, tries=%d", tries)
	}
	var he *HTTPError
	if !errors.As(err, &he) || he.Status != 422 {
		t.Fatalf("err=%v", err)
	}
}

func TestPolicyDoRespectsAttemptsAndContext(t *testing.T) {
	p := Policy{Base: time.Millisecond, Max: time.Millisecond, Attempts: 3}
	tries := 0
	if err := p.Do(context.Background(), func(int) error {
		tries++
		return errors.New("no network")
	}); err == nil || tries != 3 {
		t.Fatalf("tries=%d err=%v", tries, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	tries = 0
	err := Policy{Base: time.Hour}.Do(ctx, func(int) error {
		tries++
		cancel()
		return errors.New("no network")
	})
	if !errors.Is(err, context.Canceled) && err == nil {
		t.Fatalf("err=%v", err)
	}
	if tries != 1 {
		t.Fatalf("a cancelled context must stop immediately, tries=%d", tries)
	}
}

func TestHTTPErrorClassification(t *testing.T) {
	for status, permanent := range map[int]bool{
		400: true, 404: true, 413: true, 422: true,
		408: false, 429: false, 500: false, 502: false, 503: false,
	} {
		if got := (&HTTPError{Status: status}).Permanent(); got != permanent {
			t.Errorf("status %d permanent = %v, want %v", status, got, permanent)
		}
		if got := IsPermanent(&HTTPError{Status: status}); got != permanent {
			t.Errorf("IsPermanent(%d) = %v", status, got)
		}
	}
	if IsPermanent(errors.New("dial tcp: connection refused")) {
		t.Error("a network failure is never permanent")
	}
}

func TestClientURL(t *testing.T) {
	c := &Client{BaseURL: "https://forever.howtoguides.online/", Game: "classic_era"}
	if got := c.URL(); got != "https://forever.howtoguides.online/v1/games/classic_era/probe" {
		t.Fatalf("got %s", got)
	}
}

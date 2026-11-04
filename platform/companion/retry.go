package companion

import (
	"context"
	"time"
)

// Policy is the retry rule: transient failures (no network, 5xx, rate limit)
// are retried with a widening wait, permanent ones are not retried at all, and
// nothing is ever dropped.
type Policy struct {
	// Base is the first wait. 0 means 5s.
	Base time.Duration
	// Max caps the wait. 0 means 5m.
	Max time.Duration
	// Attempts caps the tries. 0 means keep trying until the context ends.
	Attempts int
	// OnRetry, if set, is told about each failure that will be retried.
	OnRetry func(attempt int, wait time.Duration, err error)
}

func (p Policy) base() time.Duration {
	if p.Base <= 0 {
		return 5 * time.Second
	}
	return p.Base
}

func (p Policy) max() time.Duration {
	if p.Max <= 0 {
		return 5 * time.Minute
	}
	return p.Max
}

// Wait is how long to sleep before attempt n (1 is the first retry).
func (p Policy) Wait(n int) time.Duration {
	d := p.base()
	for i := 1; i < n && d < p.max(); i++ {
		d *= 2
	}
	if d > p.max() {
		d = p.max()
	}
	return d
}

// Do runs fn until it succeeds, until it fails permanently, until the attempt
// budget runs out, or until ctx ends.
func (p Policy) Do(ctx context.Context, fn func(attempt int) error) error {
	for attempt := 1; ; attempt++ {
		err := fn(attempt)
		if err == nil {
			return nil
		}
		if IsPermanent(err) || ctx.Err() != nil {
			return err
		}
		if p.Attempts > 0 && attempt >= p.Attempts {
			return err
		}
		wait := p.Wait(attempt)
		if h, ok := err.(*HTTPError); ok && h.RetryAfter() > wait {
			wait = h.RetryAfter()
		}
		if p.OnRetry != nil {
			p.OnRetry(attempt, wait, err)
		}
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return ctx.Err()
		case <-t.C:
		}
	}
}

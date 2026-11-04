//go:build windows

package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Ohjeezoos/simforever-companion/platform/companion"
	"github.com/Ohjeezoos/simforever-companion/platform/companion/gui"
)

// guiAvailable is true on the platform that has a window.
const guiAvailable = true

// windowWriter turns the App's own output into the detail list, and keeps the
// headline saying the one thing somebody actually wants to know.
type windowWriter struct {
	w    *gui.Window
	mu   sync.Mutex
	buf  string
	sent *atomic.Int64
	last *atomic.Value // time.Time
}

func (o *windowWriter) Write(p []byte) (int, error) {
	o.mu.Lock()
	o.buf += string(p)
	parts := strings.Split(o.buf, "\n")
	o.buf = parts[len(parts)-1]
	parts = parts[:len(parts)-1]
	o.mu.Unlock()
	for _, l := range parts {
		l = strings.TrimRight(l, "\r")
		if l == "" {
			continue
		}
		o.w.Append(l)
		// The App says "sent" when something went. That is the only line worth
		// changing the headline for.
		if strings.Contains(l, "sent ") || strings.Contains(l, "uploaded") {
			o.sent.Add(1)
			o.last.Store(time.Now())
		}
	}
	return len(p), nil
}

func runGUI(dir string, cfg *companion.Config, roots []string) error {
	win, err := gui.New("Simforever Companion", companion.Version)
	if err != nil {
		return err
	}

	state, err := companion.LoadState(companion.StatePath(dir))
	if err != nil {
		return err
	}

	var sent atomic.Int64
	var last atomic.Value
	out := &windowWriter{w: win, sent: &sent, last: &last}

	agreed := make(chan struct{})
	var once sync.Once
	settle := func() {
		win.State("Syncing your data", "Leave this open while you play.", "")
		win.Note("")
	}
	win.OnAgree = func() {
		once.Do(func() { close(agreed) })
		cfg.ConsentGranted = true
		cfg.ConsentAt = time.Now().UTC().Format(time.RFC3339)
		_ = cfg.Save(dir)
		win.Notice("")
		settle()
	}

	ctx, cancel := context.WithCancel(context.Background())
	win.OnClose = cancel

	app := &companion.App{
		Dir: dir, Cfg: cfg, State: state,
		Client: companion.NewClient(cfg),
		Out:    out,
		Roots:  roots,
		// The window is the notice. The worker waits here until the button is
		// pressed, which is where the command line waited for a y.
		Consent: func() error {
			var notice strings.Builder
			companion.PrintNotice(&notice, cfg)
			win.Notice(notice.String())
			win.State("Ready when you are",
				"This sends what your game recorded about your character and your fights.", "still")
			win.ShowAgree(true)
			select {
			case <-agreed:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	}
	if cfg.ConsentGranted {
		once.Do(func() { close(agreed) })
		app.Consent = nil
		settle()
	}

	go func() {
		if err := app.Watch(ctx); err != nil && ctx.Err() == nil {
			win.Append("Stopped: " + err.Error())
			win.State("Not syncing", err.Error(), "bad")
		}
	}()

	// Keep the main status separate from individual upload messages.
	go func() {
		t := time.NewTicker(3 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
			n := sent.Load()
			if n == 0 {
				continue
			}
			when, _ := last.Load().(time.Time)
			win.State("Up to date", plural(n)+" sent"+ago(when), "good")
		}
	}()

	win.Run()
	cancel()
	return nil
}

func plural(n int64) string {
	if n == 1 {
		return "1 recording"
	}
	return fmt.Sprintf("%d recordings", n)
}

func ago(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return ", just now"
	case d < time.Hour:
		return fmt.Sprintf(", %d minutes ago", int(d.Minutes()))
	default:
		return ", " + t.Format("15:04")
	}
}

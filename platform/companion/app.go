package companion

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/Ohjeezoos/simforever-companion/platform/ingest"
	"github.com/Ohjeezoos/simforever-companion/platform/ingest/fit"
	"github.com/Ohjeezoos/simforever-companion/platform/probe"
)

// Retry a partially written SavedVariables file up to three times.
const parseRetries = 3

// rediscoverEvery is how many polls pass before the install is searched again,
// which picks up a new account folder without a restart.
const rediscoverEvery = 12

// App is one configured companion: what to watch, where to send it, and what
// has already been sent.
type App struct {
	Dir     string
	Cfg     *Config
	State   *State
	Client  *Client
	Policy  Policy
	Out     io.Writer
	Roots   []string
	DryRun  bool
	Payload string
	// Consent is called once before the first upload. nil means already given.
	Consent func() error
	// AnnounceSkips prints a line for data that was already uploaded.
	AnnounceSkips bool
	// LogStableFor overrides how long a combat log must go unchanged before it
	// is sent. Zero means LogStableFor.
	LogStableFor time.Duration
	Now          func() time.Time

	files       map[string]fileState
	saidLogsOff bool
}

type fileState struct {
	size    int64
	mod     time.Time
	fails   int
	handled bool
	ignored bool
	// stableSince is when a growing file was last seen at this size. A combat
	// log is only worth sending once the fight that is writing it has ended.
	stableSince time.Time
}

func (a *App) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

func (a *App) printf(format string, args ...any) {
	if a.Out != nil {
		fmt.Fprintf(a.Out, format+"\n", args...)
	}
}

// parseFailure marks a read the watcher should try again rather than report.
type parseFailure struct{ err error }

func (e *parseFailure) Error() string { return e.err.Error() }
func (e *parseFailure) Unwrap() error { return e.err }

// Once processes every watched file and returns, which is what -once does.
func (a *App) Once(ctx context.Context) error {
	a.AnnounceSkips = true
	return a.scan(ctx)
}

// Watch processes everything pending and then polls for changes until ctx ends.
// The game writes the file at logout and on /reload, so a change is new data.
func (a *App) Watch(ctx context.Context) error {
	interval := a.Cfg.Interval()
	a.printf("watching %d file(s) every %s, leave this running", len(a.Cfg.Watch)+len(a.combatLogsToSend()), interval)
	// Continue watching after file or server errors; stop on consent refusal or cancellation.
	if err := a.scan(ctx); err != nil {
		if isFatal(err) {
			return err
		}
		a.printf("%s", err)
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for cycle := 1; ; cycle++ {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		}
		if cycle%rediscoverEvery == 0 {
			a.rediscover()
		}
		if err := a.scan(ctx); err != nil && ctx.Err() != nil {
			return nil
		}
	}
}

// isFatal reports consent refusal or cancellation.
func isFatal(err error) bool {
	return errors.Is(err, errNoConsent) || errors.Is(err, context.Canceled)
}

func (a *App) scan(ctx context.Context) error {
	if a.files == nil {
		a.files = map[string]fileState{}
	}
	var firstErr error
	// Recheck the sharing preference even when watched paths are already saved.
	for _, path := range a.combatLogsToSend() {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := a.scanLog(ctx, path); err != nil && firstErr == nil && ctx.Err() == nil {
			firstErr = err
		}
	}
	for _, path := range a.Cfg.Watch {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		prev := a.files[path]
		same := prev.size == info.Size() && prev.mod.Equal(info.ModTime())
		if same && (prev.handled || prev.ignored) {
			continue
		}
		next := fileState{size: info.Size(), mod: info.ModTime(), handled: true}
		err = a.process(ctx, path)
		var pf *parseFailure
		switch {
		case err == nil:
		case errors.As(err, &pf):
			next.fails = 1
			if same {
				next.fails = prev.fails + 1
			}
			if next.fails == 1 {
				a.printf("%s could not be read yet (%v), trying again", path, pf.err)
			}
			if next.fails >= parseRetries {
				next.ignored = true
				a.printf("%s is not a Simforever Probe file, ignoring it until it changes", path)
			} else {
				next.handled = false
			}
		default:
			// A refusal from the server is reported and the file is left
			// alone; retrying the same bytes cannot change the answer.
			a.printf("%s: %v", path, err)
			if firstErr == nil {
				firstErr = err
			}
		}
		a.files[path] = next
	}
	return firstErr
}

// combatLogsToSend is the watched combat logs, or none when the player has not
// asked for them to be sent.
func (a *App) combatLogsToSend() []string {
	if a.Cfg.SkipCombatLogs {
		return nil
	}
	return a.Cfg.WatchLogs
}

// discoverableLogs finds logs and reports when combat-log sharing is disabled.
func (a *App) discoverableLogs() []string {
	found := CombatLogPaths(a.Roots)
	if !a.Cfg.SkipCombatLogs {
		return found
	}
	if len(found) > 0 && !a.saidLogsOff {
		a.saidLogsOff = true
		a.printf("found %d combat log(s) and will not send them: -logs=false is set", len(found))
	}
	return nil
}

// process reads one file, sends what is new, and records that it was sent.
func (a *App) process(ctx context.Context, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return &parseFailure{err}
	}
	exp, err := probe.Parse(data)
	if err != nil {
		return &parseFailure{err}
	}
	payload, err := json.MarshalIndent(exp, "", "  ")
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	hash, key := Hash(payload), EntryKey(exp, path)
	if a.Payload != "" {
		if err := os.WriteFile(a.Payload, payload, 0o644); err != nil {
			return err
		}
		a.printf("wrote the exact %d byte payload for %s to %s", len(payload), key, a.Payload)
	}
	if a.State.Seen(key, hash) {
		if a.AnnounceSkips {
			a.printf("nothing new for %s, the same data was uploaded at %s",
				key, a.State.Entries[key].UploadedAt.Local().Format(time.RFC3339))
		}
		return nil
	}
	if a.DryRun {
		run := probe.Analyze(exp, nil, probe.Options{Source: path, Coef: fit.CoefOptions{Seed: 1}})
		WriteSummary(a.Out, run.Summary(), nil)
		return nil
	}
	if a.Consent != nil {
		if err := a.Consent(); err != nil {
			return err
		}
		a.Consent = nil
	}
	var res *Result
	policy := a.Policy
	policy.OnRetry = func(attempt int, wait time.Duration, err error) {
		a.printf("upload failed (%v), retrying in %s; nothing is lost, the data stays in %s", err, wait, path)
	}
	err = policy.Do(ctx, func(int) error {
		r, e := a.Client.Upload(ctx, payload)
		res = r
		return e
	})
	if err != nil {
		return err
	}
	WriteSummary(a.Out, res.Summary, res)
	a.State.Record(key, Entry{Character: exp.Character.Name, Realm: exp.Character.Realm,
		Path: path, Hash: hash, UploadedAt: a.now()})
	return a.State.Save()
}

// scanLog waits for a combat log to stop growing before uploading it.
func (a *App) scanLog(ctx context.Context, path string) error {
	info, err := os.Stat(path)
	if err != nil || info.Size() == 0 {
		return nil
	}
	prev := a.files[path]
	now := a.now()
	grew := prev.size != info.Size() || !prev.mod.Equal(info.ModTime())
	next := fileState{size: info.Size(), mod: info.ModTime(), stableSince: prev.stableSince,
		handled: prev.handled, ignored: prev.ignored}
	if grew {
		next.stableSince, next.handled = now, false
		a.files[path] = next
		return nil
	}
	if next.ignored || next.handled {
		a.files[path] = next
		return nil
	}
	if next.stableSince.IsZero() {
		next.stableSince = now
		a.files[path] = next
		return nil
	}
	if now.Sub(next.stableSince) < a.logStableFor() {
		a.files[path] = next
		return nil
	}
	if !looksLikeCombatLog(path) {
		next.ignored = true
		a.files[path] = next
		a.printf("%s is not a combat log, ignoring it until it changes", path)
		return nil
	}
	err = a.sendLog(ctx, path, info.Size())
	next.handled = err == nil
	a.files[path] = next
	if err != nil && ctx.Err() == nil {
		a.printf("%s: %v", path, err)
	}
	return err
}

func (a *App) logStableFor() time.Duration {
	if a.LogStableFor > 0 {
		return a.LogStableFor
	}
	return LogStableFor
}

func (a *App) sendLog(ctx context.Context, path string, size int64) error {
	hash, err := HashFile(path)
	if err != nil {
		return err
	}
	key := "log:" + path
	if a.State.Seen(key, hash) {
		if a.AnnounceSkips {
			a.printf("nothing new in %s since %s", path,
				a.State.Entries[key].UploadedAt.Local().Format(time.RFC3339))
		}
		return nil
	}
	if a.DryRun {
		res, err := ingest.ParseFile(path)
		if err != nil {
			return err
		}
		a.printf("%s: %d fights, %d lines", path, len(res.Fights), res.Lines)
		return nil
	}
	if a.Consent != nil {
		if err := a.Consent(); err != nil {
			return err
		}
		a.Consent = nil
	}
	var res *LogResult
	policy := a.Policy
	policy.OnRetry = func(attempt int, wait time.Duration, err error) {
		a.printf("upload failed (%v), retrying in %s; nothing is lost, the log stays in %s", err, wait, path)
	}
	if err := policy.Do(ctx, func(int) error {
		r, e := a.Client.UploadLog(ctx, path)
		res = r
		return e
	}); err != nil {
		return err
	}
	if res.Duplicate {
		a.printf("%s was already sent (%.1f MB)", filepath.Base(path), float64(size)/(1<<20))
	} else {
		a.printf("sent %s (%.1f MB), the platform is reading it", filepath.Base(path), float64(size)/(1<<20))
	}
	a.State.Record(key, Entry{Path: path, Hash: hash, UploadedAt: a.now()})
	return a.State.Save()
}

// rediscover looks for saved variables files that appeared since the last
// search, for example a second WoW account.
func (a *App) rediscover() {
	added := false
	for _, o := range []struct {
		dst   *[]string
		found []string
		what  string
	}{
		{&a.Cfg.Watch, ProbePaths(a.Roots), "probe file"},
		{&a.Cfg.WatchLogs, a.discoverableLogs(), "combat log"},
	} {
		known := map[string]bool{}
		for _, p := range *o.dst {
			known[p] = true
		}
		for _, p := range o.found {
			if !known[p] {
				*o.dst = append(*o.dst, p)
				a.printf("found a new %s: %s", o.what, p)
				added = true
			}
		}
	}
	if added {
		if err := a.Cfg.Save(a.Dir); err != nil {
			a.printf("could not save the config: %v", err)
		}
	}
}

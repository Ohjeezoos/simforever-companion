package companion

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const sampleExport = `{
  "version": "0.1.0",
  "build": {"version": "1.15.9", "build": "69722", "toc": 11509},
  "character": {"name": "Emberlash", "realm": "Forever", "class": "MAGE", "race": "Gnome",
                "level": 60, "faction": "Alliance", "guid": "Player-4372-0000BBBB"},
  "ratings": {"spellCrit": {
     "14": {"percent": 1, "level": 60, "at": 1758067200},
     "28": {"percent": 2, "level": 60, "at": 1758067300},
     "42": {"percent": 3, "level": 60, "at": 1758067400}}}
}`

// stub answers the probe route the way platform/api does, and counts tries.
type stub struct {
	srv     *httptest.Server
	tries   atomic.Int32
	status  []int // one entry per try; the last one repeats
	lastURL atomic.Value
	body    atomic.Value
}

func newStub(t *testing.T, status ...int) *stub {
	t.Helper()
	s := &stub{status: status}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := int(s.tries.Add(1))
		s.lastURL.Store(r.URL.Path)
		b, _ := io.ReadAll(r.Body)
		s.body.Store(string(b))
		code := s.status[len(s.status)-1]
		if n <= len(s.status) {
			code = s.status[n-1]
		}
		w.Header().Set("Content-Type", "application/json")
		if code >= 300 {
			w.WriteHeader(code)
			fmt.Fprintf(w, `{"error":{"code":"nope","message":"try %d said %d"}}`, n, code)
			return
		}
		w.WriteHeader(code)
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"uploadId": 47, "build": "1.15.9.69722", "measurementIds": []int{1, 2},
			"addonVersion": "0.1.0", "clientBuild": "1.15.9.69722",
			"character":       map[string]any{"name": "Emberlash", "realm": "Forever", "class": "MAGE", "level": 60},
			"counts":          map[string]any{"snapshots": 0, "events": 0, "items": 0, "targets": 0, "ratingReadings": 3},
			"ratings":         []map[string]any{{"key": "spellCrit", "level": 60, "ratingPerPercent": 14, "n": 3, "verified": true}},
			"ratingConflicts": []any{}, "coefficients": []any{}, "coefficientsSkipped": []any{},
			"items": map[string]any{"checked": 0},
		}, "meta": map[string]any{}})
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func testApp(t *testing.T, base string, files ...string) (*App, *strings.Builder) {
	t.Helper()
	dir := t.TempDir()
	cfg, _, err := LoadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg.APIBase, cfg.Game, cfg.Watch, cfg.ConsentGranted = base, "classic_era", files, true
	if err := cfg.Save(dir); err != nil {
		t.Fatal(err)
	}
	state, err := LoadState(StatePath(dir))
	if err != nil {
		t.Fatal(err)
	}
	out := &strings.Builder{}
	return &App{Dir: dir, Cfg: cfg, State: state, Client: NewClient(cfg), Out: out,
		Policy: Policy{Base: time.Millisecond, Max: 2 * time.Millisecond, Attempts: 5},
		Now:    func() time.Time { return time.Unix(1758067400, 0).UTC() }}, out
}

func writeExport(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), SavedVariablesFile)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestUploadsThenDedupesAcrossRestarts(t *testing.T) {
	s := newStub(t, http.StatusCreated)
	path := writeExport(t, sampleExport)
	app, out := testApp(t, s.srv.URL, path)

	if err := app.Once(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := s.tries.Load(); got != 1 {
		t.Fatalf("tries = %d, want 1", got)
	}
	if p := s.lastURL.Load().(string); p != "/v1/games/classic_era/probe" {
		t.Fatalf("posted to %s", p)
	}
	if !strings.Contains(out.String(), "uploaded Emberlash-Forever") {
		t.Fatalf("no one line summary:\n%s", out)
	}
	if !json.Valid([]byte(s.body.Load().(string))) {
		t.Fatal("the payload must be JSON the server can parse")
	}

	// same file again in the same process
	if err := app.Once(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := s.tries.Load(); got != 1 {
		t.Fatalf("tries = %d after a second pass, want 1", got)
	}

	// and again after a restart, which reloads the state from disk
	restart, out2 := testApp(t, s.srv.URL, path)
	restart.Dir, restart.State = app.Dir, mustState(t, StatePath(app.Dir))
	if err := restart.Once(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := s.tries.Load(); got != 1 {
		t.Fatalf("tries = %d after a restart, want 1", got)
	}
	if !strings.Contains(out2.String(), "nothing new for emberlash-forever") {
		t.Fatalf("restart should say the data was already uploaded:\n%s", out2)
	}
}

func mustState(t *testing.T, path string) *State {
	t.Helper()
	s, err := LoadState(path)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestNewDataAfterAnUploadIsSent(t *testing.T) {
	s := newStub(t, http.StatusCreated)
	path := writeExport(t, sampleExport)
	app, _ := testApp(t, s.srv.URL, path)
	if err := app.Once(context.Background()); err != nil {
		t.Fatal(err)
	}
	grown := strings.Replace(sampleExport, `"42": {"percent": 3, "level": 60, "at": 1758067400}`,
		`"42": {"percent": 3, "level": 60, "at": 1758067400},
      "56": {"percent": 4, "level": 60, "at": 1758067500}`, 1)
	if err := os.WriteFile(path, []byte(grown), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := app.Once(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := s.tries.Load(); got != 2 {
		t.Fatalf("tries = %d, want 2", got)
	}
}

func TestServerErrorsAreRetriedAndClientErrorsAreNot(t *testing.T) {
	t.Run("5xx", func(t *testing.T) {
		s := newStub(t, http.StatusBadGateway, http.StatusInternalServerError, http.StatusCreated)
		app, out := testApp(t, s.srv.URL, writeExport(t, sampleExport))
		if err := app.Once(context.Background()); err != nil {
			t.Fatal(err)
		}
		if got := s.tries.Load(); got != 3 {
			t.Fatalf("tries = %d, want 3", got)
		}
		if !strings.Contains(out.String(), "retrying in") || !strings.Contains(out.String(), "nothing is lost") {
			t.Fatalf("retries must be announced:\n%s", out)
		}
	})
	t.Run("4xx", func(t *testing.T) {
		s := newStub(t, http.StatusUnprocessableEntity)
		app, out := testApp(t, s.srv.URL, writeExport(t, sampleExport))
		err := app.Once(context.Background())
		if err == nil {
			t.Fatal("a rejected export must be reported")
		}
		if got := s.tries.Load(); got != 1 {
			t.Fatalf("tries = %d, want 1: a 4xx must not be retried", got)
		}
		if !strings.Contains(out.String(), "422") {
			t.Fatalf("the server's reason must be printed:\n%s", out)
		}
	})
	t.Run("no network", func(t *testing.T) {
		app, _ := testApp(t, "http://127.0.0.1:1", writeExport(t, sampleExport))
		app.Policy.Attempts = 2
		if err := app.Once(context.Background()); err == nil {
			t.Fatal("an unreachable server must be reported")
		}
		if len(app.State.Entries) != 0 {
			t.Fatal("nothing may be recorded as uploaded when the upload failed")
		}
	})
}

func TestForeignFileIsIgnoredAfterAFewTries(t *testing.T) {
	s := newStub(t, http.StatusCreated)
	path := writeExport(t, "this is somebody else's addon file\n")
	app, out := testApp(t, s.srv.URL, path)
	for i := 0; i < 5; i++ {
		if err := app.Once(context.Background()); err != nil {
			t.Fatalf("a foreign file must not be an error: %v", err)
		}
	}
	if got := s.tries.Load(); got != 0 {
		t.Fatalf("tries = %d, want 0", got)
	}
	if n := strings.Count(out.String(), "is not a Simforever Probe file"); n != 1 {
		t.Fatalf("expected one ignore notice, got %d:\n%s", n, out)
	}
}

func TestAFileStillBeingWrittenIsRetriedNotIgnored(t *testing.T) {
	s := newStub(t, http.StatusCreated)
	path := writeExport(t, sampleExport[:len(sampleExport)/2])
	app, _ := testApp(t, s.srv.URL, path)
	if err := app.Once(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := s.tries.Load(); got != 0 {
		t.Fatalf("a half written file must not be uploaded, tries = %d", got)
	}
	if err := os.WriteFile(path, []byte(sampleExport), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := app.Once(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := s.tries.Load(); got != 1 {
		t.Fatalf("the finished file must be uploaded, tries = %d", got)
	}
}

func TestDryRunAndPrintPayloadUploadNothing(t *testing.T) {
	s := newStub(t, http.StatusCreated)
	payload := filepath.Join(t.TempDir(), "payload.json")
	app, out := testApp(t, s.srv.URL, writeExport(t, sampleExport))
	app.DryRun, app.Payload = true, payload
	if err := app.Once(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := s.tries.Load(); got != 0 {
		t.Fatalf("dry run uploaded %d times", got)
	}
	b, err := os.ReadFile(payload)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(b) || !strings.Contains(string(b), "Emberlash") {
		t.Fatalf("payload file is not the export:\n%s", b)
	}
	if !strings.Contains(out.String(), "would upload Emberlash-Forever") ||
		!strings.Contains(out.String(), "14 rating per 1%") {
		t.Fatalf("dry run must say what it measured:\n%s", out)
	}
	if len(app.State.Entries) != 0 {
		t.Fatal("a dry run must not mark anything as uploaded")
	}
}

func TestConsentIsAskedBeforeTheFirstUpload(t *testing.T) {
	s := newStub(t, http.StatusCreated)
	app, _ := testApp(t, s.srv.URL, writeExport(t, sampleExport))
	app.Cfg.ConsentGranted = false
	app.Consent = func() error {
		return EnsureConsent(app.Dir, app.Cfg, false, strings.NewReader(""), app.Out, time.Now)
	}
	if err := app.Once(context.Background()); err == nil {
		t.Fatal("without consent the upload must be refused")
	}
	if got := s.tries.Load(); got != 0 {
		t.Fatalf("uploaded %d times without consent", got)
	}

	app2, _ := testApp(t, s.srv.URL, app.Cfg.Watch[0])
	app2.Cfg.ConsentGranted = false
	app2.Consent = func() error {
		return EnsureConsent(app2.Dir, app2.Cfg, true, strings.NewReader(""), app2.Out, time.Now)
	}
	if err := app2.Once(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := s.tries.Load(); got != 1 {
		t.Fatalf("with -yes the upload must happen, tries = %d", got)
	}
	if back, _, _ := LoadConfig(app2.Dir); !back.ConsentGranted {
		t.Fatal("consent must be remembered")
	}
}

func TestWatchPicksUpAChange(t *testing.T) {
	s := newStub(t, http.StatusCreated)
	path := writeExport(t, sampleExport)
	app, _ := testApp(t, s.srv.URL, path)
	app.Cfg.PollSeconds = 1
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- app.Watch(ctx) }()

	waitFor(t, func() bool { return s.tries.Load() == 1 })
	grown := strings.Replace(sampleExport, `"level": 60, "faction"`, `"level": 61, "faction"`, 1)
	if err := os.WriteFile(path, []byte(grown), 0o644); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return s.tries.Load() == 2 })
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); {
		if ok() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("timed out waiting for the watcher")
}

// A startup upload failure must leave the watcher running.
func TestOneRejectedFileDoesNotStopTheWatcher(t *testing.T) {
	s := newStub(t, http.StatusUnprocessableEntity)
	app, out := testApp(t, s.srv.URL, writeExport(t, sampleExport))
	app.Cfg.PollSeconds = 1

	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- app.Watch(ctx) }()
	select {
	case err := <-done:
		if ctx.Err() == nil {
			t.Fatalf("the watcher stopped on a rejected file: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the watcher neither stopped nor honoured the context")
	}
	if !strings.Contains(out.String(), "422") && !strings.Contains(out.String(), "server returned") {
		t.Errorf("the rejection was not reported to the player:\n%s", out.String())
	}
}

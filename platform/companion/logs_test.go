package companion

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const sampleLog = `9/20 21:14:02.113  SPELL_CAST_SUCCESS,Player-4613-0062E7DB,"Ohjeez",0x511,0x0,Creature-0-4613-1-1-299-0,"Kobold Miner",0xa48,0x0,23881,"Bloodthirst",0x1
9/20 21:14:02.113  SPELL_DAMAGE,Player-4613-0062E7DB,"Ohjeez",0x511,0x0,Creature-0-4613-1-1-299-0,"Kobold Miner",0xa48,0x0,23881,"Bloodthirst",0x1,142,0,1,0,0,0,nil,nil,nil
9/20 21:14:03.550  SWING_DAMAGE,Player-4613-0062E7DB,"Ohjeez",0x511,0x0,Creature-0-4613-1-1-299-0,"Kobold Miner",0xa48,0x0,71,0,1,0,0,0,nil,nil,nil
`

// logStub answers the combat log route the way platform/api does.
type logStub struct {
	srv      *httptest.Server
	tries    atomic.Int32
	filename atomic.Value
	size     atomic.Int64
}

func newLogStub(t *testing.T) *logStub {
	t.Helper()
	s := &logStub{}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.tries.Add(1)
		_, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil {
			t.Errorf("content type: %v", err)
		}
		mr := multipart.NewReader(r.Body, params["boundary"])
		p, err := mr.NextPart()
		if err != nil {
			t.Errorf("no file part: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		s.filename.Store(p.FileName())
		n, _ := io.Copy(io.Discard, p)
		s.size.Store(n)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"uploadId": 9, "jobId": 11, "sha256": "abc", "duplicate": false, "sizeBytes": n,
		}, "meta": map[string]any{}})
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func logApp(t *testing.T, path string, base string, now func() time.Time) *App {
	t.Helper()
	dir := t.TempDir()
	cfg := &Config{APIBase: base, Game: "forever", WatchLogs: []string{path},
		ConsentGranted: true}
	cfg.setDefaults()
	state, err := LoadState(StatePath(dir))
	if err != nil {
		t.Fatal(err)
	}
	return &App{Dir: dir, Cfg: cfg, State: state, Client: NewClient(cfg), Out: io.Discard,
		LogStableFor: time.Minute, Now: now,
		Policy: Policy{Base: time.Millisecond, Max: time.Millisecond, Attempts: 1}}
}

// The game appends to the log for as long as the player is fighting. Sending it
// then would send the same fight repeatedly, each time half written.
func TestCombatLogIsSentOnlyOnceItStopsGrowing(t *testing.T) {
	stub := newLogStub(t)
	path := filepath.Join(t.TempDir(), CombatLogFile)
	if err := os.WriteFile(path, []byte(sampleLog), 0o644); err != nil {
		t.Fatal(err)
	}
	clock := time.Unix(1700000000, 0)
	app := logApp(t, path, stub.srv.URL, func() time.Time { return clock })
	ctx := context.Background()

	if err := app.scan(ctx); err != nil {
		t.Fatal(err)
	}
	if got := stub.tries.Load(); got != 0 {
		t.Fatalf("sent a log that had only just appeared: %d", got)
	}

	// Still being written: the fight is not over.
	clock = clock.Add(30 * time.Second)
	if err := os.WriteFile(path, []byte(sampleLog+sampleLog), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := app.scan(ctx); err != nil {
		t.Fatal(err)
	}
	if got := stub.tries.Load(); got != 0 {
		t.Fatalf("sent a log that was still growing: %d", got)
	}

	// Unchanged for long enough: the fight ended.
	clock = clock.Add(2 * time.Minute)
	if err := app.scan(ctx); err != nil {
		t.Fatal(err)
	}
	if got := stub.tries.Load(); got != 1 {
		t.Fatalf("a settled log was not sent once: %d", got)
	}
	if got := stub.filename.Load(); got != CombatLogFile {
		t.Fatalf("sent under the wrong name: %v", got)
	}
	if got, want := stub.size.Load(), int64(len(sampleLog)*2); got != want {
		t.Fatalf("sent %d bytes, want %d", got, want)
	}

	// Nothing new: a settled log is not sent again.
	clock = clock.Add(5 * time.Minute)
	if err := app.scan(ctx); err != nil {
		t.Fatal(err)
	}
	if got := stub.tries.Load(); got != 1 {
		t.Fatalf("an unchanged log was sent again: %d", got)
	}
}

// A second night of play appends to the same file, and that is new data.
func TestCombatLogIsSentAgainAfterItGrows(t *testing.T) {
	stub := newLogStub(t)
	path := filepath.Join(t.TempDir(), CombatLogFile)
	if err := os.WriteFile(path, []byte(sampleLog), 0o644); err != nil {
		t.Fatal(err)
	}
	clock := time.Unix(1700000000, 0)
	app := logApp(t, path, stub.srv.URL, func() time.Time { return clock })
	ctx := context.Background()
	for _, step := range []time.Duration{0, 2 * time.Minute} {
		clock = clock.Add(step)
		if err := app.scan(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if got := stub.tries.Load(); got != 1 {
		t.Fatalf("first send: %d", got)
	}
	if err := os.WriteFile(path, []byte(sampleLog+sampleLog+sampleLog), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, step := range []time.Duration{time.Minute, 2 * time.Minute} {
		clock = clock.Add(step)
		if err := app.scan(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if got := stub.tries.Load(); got != 2 {
		t.Fatalf("a log that grew was not sent again: %d", got)
	}
}

// A file that merely shares the name is left where it is.
func TestSomethingElseNamedLikeACombatLogIsLeftAlone(t *testing.T) {
	stub := newLogStub(t)
	path := filepath.Join(t.TempDir(), CombatLogFile)
	if err := os.WriteFile(path, []byte("this is not a combat log\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	clock := time.Unix(1700000000, 0)
	app := logApp(t, path, stub.srv.URL, func() time.Time { return clock })
	for _, step := range []time.Duration{0, 2 * time.Minute} {
		clock = clock.Add(step)
		if err := app.scan(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if got := stub.tries.Load(); got != 0 {
		t.Fatalf("sent a file that is not a combat log: %d", got)
	}
}

// Timestamped combat-log sessions must be discovered alongside the fixed filename.
func TestCombatLogPathsFindsASessionsOwnLog(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "_classic_beta_", "Logs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"WoWCombatLog-092026_150957.txt",
		"WoWCombatLog-092026_021119.txt",
		"WoWCombatLog.txt",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(sampleLog), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Something else in the same folder is not a combat log.
	if err := os.WriteFile(filepath.Join(dir, "Hotfix.log"), []byte("no"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := CombatLogPaths([]string{root})
	if len(got) != 3 {
		t.Fatalf("found %d logs, want the three that are logs: %v", len(got), got)
	}
	for _, p := range got {
		if !strings.Contains(filepath.Base(p), "WoWCombatLog") {
			t.Fatalf("that is not a combat log: %s", p)
		}
	}
}

func TestCombatLogPathsFindsOneFlavourEach(t *testing.T) {
	root := t.TempDir()
	for _, flavour := range []string{"_classic_beta_", "_classic_era_"} {
		dir := filepath.Join(root, flavour, "Logs")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, CombatLogFile), []byte(sampleLog), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// A flavour that has never logged has no file and must not be invented.
	if err := os.MkdirAll(filepath.Join(root, "_retail_", "Logs"), 0o755); err != nil {
		t.Fatal(err)
	}
	got := CombatLogPaths([]string{root})
	if len(got) != 2 {
		t.Fatalf("found %d logs, want 2: %v", len(got), got)
	}
	for _, p := range got {
		if !strings.HasSuffix(p, CombatLogFile) {
			t.Fatalf("not a combat log: %s", p)
		}
	}
}

func TestLooksLikeCombatLog(t *testing.T) {
	for _, c := range []struct {
		name string
		body string
		want bool
	}{
		{"classic stamp", sampleLog, true},
		{"stamp with year and zone", "9/20/2026 21:14:02.113-4  SWING_DAMAGE,a,b\n", true},
		{"byte order mark", "\xef\xbb\xbf" + sampleLog, true},
		{"prose", "this is not a combat log\n", false},
		{"empty", "", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), CombatLogFile)
			if err := os.WriteFile(path, []byte(c.body), 0o644); err != nil {
				t.Fatal(err)
			}
			if got := looksLikeCombatLog(path); got != c.want {
				t.Fatalf("got %v, want %v", got, c.want)
			}
		})
	}
}

// The route the companion posts to has to be the one the platform serves.
func TestLogURL(t *testing.T) {
	c := &Client{BaseURL: "https://example.test/", Game: "forever"}
	if got, want := c.LogURL(), "https://example.test/v1/games/forever/logs"; got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}

// Combat-log sharing defaults on; -logs=false must prevent uploads.
func TestCombatLogsGoUnlessTurnedOff(t *testing.T) {
	stub := newLogStub(t)
	path := filepath.Join(t.TempDir(), CombatLogFile)
	if err := os.WriteFile(path, []byte(sampleLog), 0o644); err != nil {
		t.Fatal(err)
	}
	clock := time.Unix(1700000000, 0)
	app := logApp(t, path, stub.srv.URL, func() time.Time { return clock })
	ctx := context.Background()

	// A config that says nothing about combat logs sends them. Two scans,
	// because a log has to be seen unchanged before it counts as finished.
	if err := app.scan(ctx); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(5 * time.Minute)
	if err := app.scan(ctx); err != nil {
		t.Fatal(err)
	}
	if got := stub.tries.Load(); got != 1 {
		t.Fatalf("a settled log was not sent with the default config: %d", got)
	}

	// And SkipCombatLogs stops them, including ones already in the watch list.
	app.Cfg.SkipCombatLogs = true
	if err := os.WriteFile(path, []byte(sampleLog+sampleLog), 0o644); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(5 * time.Minute)
	if err := app.scan(ctx); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(5 * time.Minute)
	if err := app.scan(ctx); err != nil {
		t.Fatal(err)
	}
	if got := stub.tries.Load(); got != 1 {
		t.Fatalf("a combat log went with -logs=false set: %d", got)
	}
}

package companion

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestConfigFirstRunCreatesDefaults(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cfg")
	cfg, created, err := LoadConfig(dir)
	if err != nil || !created {
		t.Fatalf("created=%v err=%v", created, err)
	}
	if cfg.APIBase != DefaultAPIBase || cfg.Game != DefaultGame || cfg.PollSeconds != DefaultPollSeconds {
		t.Fatalf("defaults not applied: %+v", cfg)
	}
	if cfg.ConsentGranted {
		t.Fatal("consent must start off")
	}
	if _, err := os.Stat(ConfigPath(dir)); err != nil {
		t.Fatalf("config file not written: %v", err)
	}
}

func TestConfigRoundTrip(t *testing.T) {
	dir := t.TempDir()
	cfg, _, err := LoadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg.APIBase = "http://127.0.0.1:9999"
	cfg.Game = "forever"
	cfg.DisplayName = "Emberlash"
	cfg.WoWPath = "/mnt/c/Program Files (x86)/World of Warcraft"
	cfg.Watch = []string{"/a/SimforeverProbe.lua", "/b/SimforeverProbe.lua"}
	cfg.PollSeconds = 11
	if err := cfg.Grant(dir, time.Date(2026, 9, 18, 10, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}

	back, created, err := LoadConfig(dir)
	if err != nil || created {
		t.Fatalf("created=%v err=%v", created, err)
	}
	if back.APIBase != cfg.APIBase || back.Game != cfg.Game || back.DisplayName != cfg.DisplayName ||
		back.WoWPath != cfg.WoWPath || back.PollSeconds != 11 || len(back.Watch) != 2 ||
		back.Watch[1] != cfg.Watch[1] {
		t.Fatalf("round trip lost data: %+v", back)
	}
	if !back.ConsentGranted || back.ConsentAt != "2026-09-18T10:00:00Z" {
		t.Fatalf("consent not persisted: %+v", back)
	}
	if back.Interval() != 11*time.Second {
		t.Fatalf("interval = %s", back.Interval())
	}
}

func TestConfigIsValidJSONAndReportsBadJSON(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ConfigPath(dir), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadConfig(dir); err == nil {
		t.Fatal("a broken config should be reported, not silently replaced")
	}
}

// Known default API addresses migrate; custom addresses remain unchanged.
func TestADomainMoveCarriesInstallsThatNeverChoseOne(t *testing.T) {
	const moved = "https://simforever.example"
	shipped := PreviousAPIBases
	def := DefaultAPIBase
	t.Cleanup(func() { PreviousAPIBases = shipped })
	// Stand in for the next release: the old default becomes a previous one.
	PreviousAPIBases = append([]string{def}, shipped...)

	for _, tc := range []struct {
		name  string
		start string
		want  string
	}{
		{"never chose one", def, moved},
		{"empty", "", moved},
		{"chose their own", "https://my-own-box.invalid", "https://my-own-box.invalid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &Config{APIBase: tc.start}
			c.setDefaultsWith(moved)
			if c.APIBase != tc.want {
				t.Errorf("started at %q, want %q, got %q", tc.start, tc.want, c.APIBase)
			}
		})
	}
}


// Old default game keys migrate to Forever.
func TestTheGameKeyMovesOnFromADefaultNobodyChose(t *testing.T) {
	for _, tc := range []struct {
		name  string
		start string
		want  string
	}{
		{"holding the old default", "classic_era", DefaultGame},
		{"empty", "", DefaultGame},
		{"chose their own", "some_other_game", "some_other_game"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &Config{Game: tc.start}
			c.setDefaults()
			if c.Game != tc.want {
				t.Errorf("started at %q, want %q, got %q", tc.start, tc.want, c.Game)
			}
		})
	}
}

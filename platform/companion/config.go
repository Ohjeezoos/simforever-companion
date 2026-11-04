// Package companion watches addon recordings and uploads them to Simforever.
package companion

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// Version is stamped by the build through -ldflags -X; development builds use dev.
var Version = "dev"

const (
	// DefaultAPIBase is the public platform.
	DefaultAPIBase = "https://forever.howtoguides.online"
	// DefaultGame is the Forever upload route key.
	DefaultGame = "forever"
	// DefaultPollSeconds is how often the watched files are stat'd.
	DefaultPollSeconds = 5

	configName = "config.json"
	stateName  = "state.json"
	dirName    = "simforever-companion"
)

// PreviousAPIBases lists old defaults eligible for automatic migration.
var PreviousAPIBases = []string{
	"https://forever.howtoguides.online",
}

// PreviousGames lists old default game keys eligible for migration.
var PreviousGames = []string{
	"classic_era",
}

type Config struct {
	APIBase        string   `json:"apiBase"`
	Game           string   `json:"game"`
	DisplayName    string   `json:"displayName,omitempty"`
	WoWPath        string   `json:"wowPath,omitempty"`
	Watch          []string `json:"watch"`
	WatchLogs      []string `json:"watchLogs"`
	// migrated tells the caller to persist updated default settings.
	migrated bool

	// SkipCombatLogs disables combat-log sharing. Its zero value retains the existing default.
	SkipCombatLogs bool `json:"skipCombatLogs,omitempty"`
	PollSeconds    int      `json:"pollSeconds"`
	ConsentGranted bool     `json:"consentGranted"`
	ConsentAt      string   `json:"consentAt,omitempty"`
}

// DefaultDir is where the config and state live when -config is not given.
func DefaultDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		home, herr := os.UserHomeDir()
		if herr != nil {
			return "", err
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, dirName), nil
}

func ConfigPath(dir string) string { return filepath.Join(dir, configName) }
func StatePath(dir string) string  { return filepath.Join(dir, stateName) }

// Migrated reports whether loading moved a value this program had written
// itself, so a caller that can save should.
func (c *Config) Migrated() bool { return c.migrated }

// LoadConfig reads the config, creating it with defaults on first run. The
// second result reports whether this run created it.
func LoadConfig(dir string) (*Config, bool, error) {
	cfg := &Config{}
	b, err := os.ReadFile(ConfigPath(dir))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		cfg.setDefaults()
		if err := cfg.Save(dir); err != nil {
			return nil, false, err
		}
		return cfg, true, nil
	case err != nil:
		return nil, false, err
	}
	if err := json.Unmarshal(b, cfg); err != nil {
		return nil, false, fmt.Errorf("%s: %w", ConfigPath(dir), err)
	}
	cfg.setDefaults()
	return cfg, false, nil
}

func (c *Config) setDefaults() { c.setDefaultsWith(DefaultAPIBase) }

// setDefaultsWith is setDefaults against a given default address, so a release
// that moves the platform can be tested before it is made.
func (c *Config) setDefaultsWith(defaultAPIBase string) {
	// Migrate known service defaults; preserve custom API addresses.
	if c.APIBase == "" {
		c.APIBase = defaultAPIBase
	} else if c.APIBase != defaultAPIBase {
		for _, shipped := range PreviousAPIBases {
			if c.APIBase == shipped {
				c.APIBase, c.migrated = defaultAPIBase, true
				break
			}
		}
	}
	if c.Game == "" {
		c.Game = DefaultGame
	} else if c.Game != DefaultGame {
		for _, shipped := range PreviousGames {
			if c.Game == shipped {
				c.Game, c.migrated = DefaultGame, true
				break
			}
		}
	}
	if c.PollSeconds <= 0 {
		c.PollSeconds = DefaultPollSeconds
	}
	if c.Watch == nil {
		c.Watch = []string{}
	}
	if c.WatchLogs == nil {
		c.WatchLogs = []string{}
	}
}

func (c *Config) Save(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(ConfigPath(dir), append(b, '\n'))
}

// Grant records consent so it is asked for once and never again.
func (c *Config) Grant(dir string, now time.Time) error {
	c.ConsentGranted = true
	c.ConsentAt = now.UTC().Format(time.RFC3339)
	return c.Save(dir)
}

func (c *Config) Interval() time.Duration { return time.Duration(c.PollSeconds) * time.Second }

// writeFileAtomic keeps the config and state readable even if the machine dies
// mid-write, which matters because the state is what stops a re-upload.
func writeFileAtomic(path string, b []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

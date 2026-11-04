// simforever-companion watches the Simforever Probe addon's saved variables
// and uploads what the game client recorded to the simforever platform.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Ohjeezoos/simforever-companion/platform/companion"
)

// isSet reports whether a flag was given on the command line, so an absent
// -logs leaves a decision already made alone instead of silently undoing it.
func isSet(name string) bool {
	seen := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == name {
			seen = true
		}
	})
	return seen
}

func main() {
	err := run()
	if err != nil {
		fmt.Fprintln(os.Stderr, "simforever-companion: "+err.Error())
	}
	// Keep startup errors visible when launched without a parent terminal.
	holdWindowOpen()
	if err != nil {
		os.Exit(1)
	}
}

func run() error {
	var (
		wow      = flag.String("wow", "", "path to the World of Warcraft install, a flavour folder, or the SavedVariables file itself")
		dirFlag  = flag.String("config", "", "directory holding config.json and state.json (default: the user config dir)")
		api      = flag.String("api", "", "platform base URL (default "+companion.DefaultAPIBase+")")
		game     = flag.String("game", "", "game key the upload is filed under (default "+companion.DefaultGame+")")
		name     = flag.String("name", "", "optional display name recorded with the upload")
		interval = flag.Duration("interval", 0, "how often the watched files are checked (default 5s)")
		show     = flag.Bool("show", false, "print the config path and what would be watched, then exit")
		once     = flag.Bool("once", false, "upload anything pending and exit")
		dryRun   = flag.Bool("dry-run", false, "parse and print what would be measured, upload nothing, then exit")
		yes      = flag.Bool("yes", false, "agree to the upload notice without being asked")
	sendLogs = flag.Bool("logs", true, "send your combat log as well as your character (-logs=false to keep it on your machine)")
		payload  = flag.String("print-payload", "", "write the exact JSON that would be uploaded to this file")
		showVer  = flag.Bool("version", false, "print the version and exit")
	console  = flag.Bool("console", false, "stay on the command line instead of opening the window")
	)
	flag.Parse()
	if *showVer {
		fmt.Println("simforever-companion " + companion.Version)
		return nil
	}

	dir := *dirFlag
	if dir == "" {
		d, err := companion.DefaultDir()
		if err != nil {
			return err
		}
		dir = d
	}
	cfg, created, err := companion.LoadConfig(dir)
	if err != nil {
		return err
	}
	changed := created || cfg.Migrated()
	for _, o := range []struct {
		dst *string
		val string
	}{
		{&cfg.APIBase, *api}, {&cfg.Game, *game}, {&cfg.DisplayName, *name}, {&cfg.WoWPath, *wow},
	} {
		if o.val != "" && *o.dst != o.val {
			*o.dst, changed = o.val, true
		}
	}
	if *interval > 0 && int(interval.Seconds()) != cfg.PollSeconds {
		cfg.PollSeconds, changed = max(1, int(interval.Seconds())), true
	}
	if isSet("logs") && *sendLogs == cfg.SkipCombatLogs {
		cfg.SkipCombatLogs, changed = !*sendLogs, true
	}

	roots, found, logs := companion.Discover(cfg.WoWPath)
	if cfg.SkipCombatLogs {
		logs = nil
	}
	for _, o := range []struct {
		dst   *[]string
		found []string
	}{{&cfg.Watch, found}, {&cfg.WatchLogs, logs}} {
		known := map[string]bool{}
		for _, p := range *o.dst {
			known[p] = true
		}
		for _, p := range o.found {
			if !known[p] {
				*o.dst, changed = append(*o.dst, p), true
			}
		}
	}
	if changed {
		if err := cfg.Save(dir); err != nil {
			return err
		}
	}

	// Use the window for persistent sessions and the terminal for one-shot commands.
	oneShot := *show || *once || *dryRun || *payload != "" || *showVer
	if guiAvailable && !oneShot && !*console {
		return runGUI(dir, cfg, roots)
	}

	if *show {
		printShow(dir, cfg, roots)
		return nil
	}
	if len(cfg.Watch) == 0 && len(cfg.WatchLogs) == 0 {
		fmt.Printf("no %s found. Searched:\n", companion.SavedVariablesFile)
		for _, r := range roots {
			fmt.Println("  " + r)
		}
		fmt.Println("Install the addon and log in once, or point at the install with -wow.")
		if *once || *dryRun {
			return nil
		}
	}

	state, err := companion.LoadState(companion.StatePath(dir))
	if err != nil {
		return err
	}
	app := &companion.App{Dir: dir, Cfg: cfg, State: state, Client: companion.NewClient(cfg),
		Out: os.Stdout, Roots: roots, DryRun: *dryRun, Payload: *payload}

	if !cfg.ConsentGranted {
		companion.PrintNotice(os.Stdout, cfg)
	}
	if !*dryRun {
		app.Consent = func() error {
			return companion.EnsureConsent(dir, cfg, *yes, os.Stdin, os.Stdout, time.Now)
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if *once || *dryRun {
		app.Policy = companion.Policy{Base: 2 * time.Second, Max: 30 * time.Second, Attempts: 4}
		return app.Once(ctx)
	}
	app.Policy = companion.Policy{Base: 5 * time.Second, Max: 5 * time.Minute}
	return app.Watch(ctx)
}

func printShow(dir string, cfg *companion.Config, roots []string) {
	fmt.Printf("combat log: %s\n", map[bool]string{
		true:  "not sent (-logs=false is set)",
		false: "sent, along with your character",
	}[cfg.SkipCombatLogs])
	fmt.Println("config:   " + companion.ConfigPath(dir))
	fmt.Println("state:    " + companion.StatePath(dir))
	fmt.Println("api:      " + cfg.APIBase + "/v1/games/" + cfg.Game + "/probe")
	fmt.Printf("interval: %s\n", cfg.Interval())
	if cfg.DisplayName != "" {
		fmt.Println("name:     " + cfg.DisplayName)
	}
	consent := "not given yet, the first upload will ask"
	if cfg.ConsentGranted {
		consent = "given " + cfg.ConsentAt
	}
	fmt.Println("consent:  " + consent)
	fmt.Println("searched:")
	for _, r := range roots {
		fmt.Println("  " + r)
	}
	if len(cfg.Watch) == 0 && len(cfg.WatchLogs) == 0 {
		fmt.Println("watching: nothing found yet")
		return
	}
	fmt.Println("watching:")
	for _, p := range append(append([]string{}, cfg.Watch...), cfg.WatchLogs...) {
		note := ""
		if st, err := os.Stat(p); err != nil {
			note = "  (missing right now)"
		} else {
			note = fmt.Sprintf("  (%d bytes, changed %s)", st.Size(), st.ModTime().Local().Format(time.RFC3339))
		}
		fmt.Println("  " + p + note)
	}
}

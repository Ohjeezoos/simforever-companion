package companion

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// ConsentNotice is printed verbatim on first run. It is the whole promise: a
// player has to be able to read it and know exactly what leaves their machine.
const ConsentNotice = `Simforever companion, first run.

This program watches one file that the Simforever Probe addon writes, and
uploads what the addon recorded about YOUR character to %s

The addon's file is read and taken apart here; only these come out of it and go:
  - your character's name, realm, class, race, level, faction and talent string
  - your equipped gear: item ids, item links and item levels
  - the client's own stat table for the items you equipped
  - your character sheet stats, and every rating amount next to the percentage
    the client says it is worth, which is the number this project exists to find
  - your own combat events: your casts, your hits and heals, and the name,
    level and type of what you fought
  - the game client build and the addon version

It never sends:
  - anything the addon recorded about other players, which it does record: who
    you grouped with and who you inspected stays on your machine
  - any chat, whisper, guild or party conversation
  - your account name, email, password, or anything from Battle.net

It also sends your combat log, the WoWCombatLog.txt the game writes while combat
logging is on. That file is how a fight is measured, and it is not yours alone:
it names everybody who was near you and what they did. It is sent as the game
wrote it. Start the program with -logs=false to keep it on your machine.

It only reads. It never writes to the game and never acts for you.

Before you allow anything, you can see the exact bytes for yourself:
  -dry-run         parse and print what would be measured, upload nothing
  -print-payload FILE   write the exact JSON that would be uploaded

`

// errNoConsent is the one refusal message, so a player always reads the same
// way out of it.
var errNoConsent = errors.New("nothing was uploaded: answer y, or re-run with -yes, once you have read the notice above")

// PrintNotice writes the notice for this configuration.
func PrintNotice(w io.Writer, cfg *Config) {
	fmt.Fprintf(w, ConsentNotice, strings.TrimRight(cfg.APIBase, "/"))
}

// EnsureConsent asks once, before the first upload ever happens. yes comes
// from the -yes flag; otherwise a terminal is asked and anything else refuses.
func EnsureConsent(dir string, cfg *Config, yes bool, in io.Reader, w io.Writer, now func() time.Time) error {
	if cfg.ConsentGranted {
		return nil
	}
	if yes {
		fmt.Fprintln(w, "consent given on the command line with -yes")
		return cfg.Grant(dir, now())
	}
	if !interactive(in) {
		return errNoConsent
	}
	fmt.Fprint(w, "Upload this data from now on? [y/N]: ")
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && strings.TrimSpace(line) == "" {
		fmt.Fprintln(w)
		return errNoConsent
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return cfg.Grant(dir, now())
	}
	return errNoConsent
}

// interactive reports whether we can ask a human, without a terminal library.
func interactive(in io.Reader) bool {
	f, ok := in.(*os.File)
	if !ok {
		return true
	}
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

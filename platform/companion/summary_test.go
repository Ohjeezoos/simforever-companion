package companion

import (
	"strings"
	"testing"

	"github.com/Ohjeezoos/simforever-companion/platform/probe"
)

func TestWords(t *testing.T) {
	for in, want := range map[string]string{
		"spellCrit": "spell crit", "weaponDPS": "weapon dps", "statArmor": "stat armor",
		"expertise": "expertise", "cr32": "cr32", "": "unnamed",
		"Armor": "armor", "WeaponDPS": "weapon dps", "DPS": "dps",
	} {
		if got := words(in); got != want {
			t.Errorf("words(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHeadlineIsOneLine(t *testing.T) {
	s := probe.Summary{
		Character: map[string]any{"name": "Emberlash", "realm": "Forever", "class": "MAGE", "level": float64(60)},
		Ratings:   []probe.RatingFit{{Key: "spellCrit", Level: 60, PerPercent: 14, N: 3}},
	}
	line := Headline(s, &Result{UploadID: 47, MeasurementIDs: []int32{1, 2}})
	if strings.Contains(line, "\n") {
		t.Fatalf("not one line: %q", line)
	}
	for _, want := range []string{"uploaded Emberlash-Forever (Mage level 60)", "1 rating conversion", "2 measurements stored", "(upload 47)"} {
		if !strings.Contains(line, want) {
			t.Errorf("headline %q missing %q", line, want)
		}
	}
	if got := Headline(s, nil); !strings.HasPrefix(got, "would upload ") {
		t.Errorf("dry run headline = %q", got)
	}
}

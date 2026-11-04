package ingest

import (
	"strings"
	"testing"
)

// A creature never says its level on the line where it was missed, so the
// level has to be remembered from a line where it did say. This client states
// it on the swing that landed, where the advanced block belongs to the thing
// that was hit rather than to the attacker. Taken verbatim from a Forever log.
func TestACreaturesLevelIsRemembered(t *testing.T) {
	line := "9/20/2026 18:29:45.4772  SWING_DAMAGE_LANDED,Player-4619-0090351C," +
		`"Keldath-ClassicBetaPvP-",0x518,0x80000000,` +
		"Creature-0-6782-1-11803-3123-0000300814," + `"Bloodtalon Scythemaw",0xa48,0x80000000,` +
		"Creature-0-6782-1-11803-3123-0000300814,0000000000000000,104,208,32,0,518,0,0,0,1,0,0,0," +
		"1290.38,-4725.66,1414,1.5917,10,13,18,-1,1,0,0,0,nil,nil,nil"
	res, err := Parse(strings.NewReader(line+"\n"), ParseOptions{Year: 2026})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	const creature = "Creature-0-6782-1-11803-3123-0000300814"
	if got := res.Levels[creature]; got != 10 {
		t.Fatalf("the scythemaw is level %d, the log says 10", got)
	}
	// The attacker is not described by this line, so nothing about it may be
	// taken from it.
	if _, ok := res.Levels["Player-4619-0090351C"]; ok {
		t.Error("the attacker's level was taken from a line that describes the thing it hit")
	}
}

// The other way round: when the advanced block describes the attacker, the
// trailing field is not the attacker's level if the attacker is a player.
// Across 574 lines of one Forever log it stayed at 2 for a character the addon
// recorded going from level 11 to level 12, so it is some other number and
// nothing about a player's level may be taken from it. Line verbatim from a log.
func TestAPlayersOwnLevelIsNeverStated(t *testing.T) {
	line := "9/20/2026 02:17:06.2412  SWING_DAMAGE,Player-4613-00B95936," +
		`"Duste-ClassicBetaPvP2-",0x518,0x80000000,` +
		"Creature-0-4621-1-12704-5676-00002F25FE," + `"Summoned Voidwalker",0xa28,0x80000000,` +
		"Player-4613-00B95936,0000000000000000,278,278,62,16,148,0,0,0,0,428,428,0," +
		"1805.81,-4376.58,1454,1.7709,6,4,9,-1,1,0,0,0,nil,1,nil"
	res, err := Parse(strings.NewReader(line+"\n"), ParseOptions{Year: 2026})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if lvl, ok := res.Levels["Player-4613-00B95936"]; ok {
		t.Errorf("the attacker was given level %d from a field that is not its level", lvl)
	}
	if _, ok := res.Levels["Creature-0-4621-1-12704-5676-00002F25FE"]; ok {
		t.Error("the voidwalker's level was taken from a line that describes the attacker")
	}
}

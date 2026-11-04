package probe

import "testing"

func snapWith(id int32, talents string, crit float64, gear []GearPiece) Snapshot {
	return Snapshot{
		ID: id, At: int64(id), Level: 60, Talents: talents, Gear: gear,
		Combat: CombatStats{CritChance: crit},
	}
}

var oneItem = []GearPiece{{Slot: 1, ID: 12640, ItemString: "item:12640:0:0:0"}}

// The whole point: a point went into a talent, the sheet moved, and that is
// what the point is worth. The client never states this.
func TestOnePointInOneTalentIsMeasuredFromTheSheet(t *testing.T) {
	snaps := []Snapshot{
		snapWith(1, "00000-00000-00000", 10, oneItem),
		snapWith(2, "00100-00000-00000", 11, oneItem),
	}
	fits := FitTalents(snaps)
	if len(fits) != 1 {
		t.Fatalf("got %d fits, want 1: %+v", len(fits), fits)
	}
	f := fits[0]
	if f.Key != "1/3" || f.Stat != "critChance" {
		t.Fatalf("wrong talent or stat: %+v", f)
	}
	if f.PerPoint != 1 || f.N != 1 || f.Conflict != "" {
		t.Fatalf("want 1 crit per point: %+v", f)
	}
}

// Five points at once still measure one point, because that is the number the
// simulator needs and the one the client does not give.
func TestPointsAreDividedOut(t *testing.T) {
	fits := FitTalents([]Snapshot{
		snapWith(1, "00000", 10, oneItem),
		snapWith(2, "00500", 15, oneItem),
	})
	if len(fits) != 1 || fits[0].PerPoint != 1 {
		t.Fatalf("five points worth five crit is one per point: %+v", fits)
	}
}

// A number with two possible causes is not a measurement. Gear, level and a
// second talent each rule a pair out.
func TestAPairWithASecondReasonIsNotMeasured(t *testing.T) {
	other := []GearPiece{{Slot: 1, ID: 99, ItemString: "item:99:0:0:0"}}
	cases := []struct {
		name string
		a, b Snapshot
	}{
		{"gear changed too", snapWith(1, "00000", 10, oneItem), snapWith(2, "00100", 11, other)},
		{"two talents changed", snapWith(1, "00000", 10, oneItem), snapWith(2, "00110", 11, oneItem)},
	}
	for _, c := range cases {
		if fits := FitTalents([]Snapshot{c.a, c.b}); len(fits) != 0 {
			t.Errorf("%s: measured anyway: %+v", c.name, fits)
		}
	}
	levelUp := snapWith(2, "00100", 11, oneItem)
	levelUp.Level = 61
	if fits := FitTalents([]Snapshot{snapWith(1, "00000", 10, oneItem), levelUp}); len(fits) != 0 {
		t.Errorf("level changed too: measured anyway: %+v", fits)
	}
}

// What one point buys is a constant. Two readings that disagree are a fault to
// report, never something to average.
func TestDisagreeingReadingsAreAConflict(t *testing.T) {
	fits := FitTalents([]Snapshot{
		snapWith(1, "00000", 10, oneItem),
		snapWith(2, "00100", 11, oneItem),
		snapWith(3, "00000", 11, oneItem),
		snapWith(4, "00100", 15, oneItem),
	})
	if len(fits) != 1 {
		t.Fatalf("got %d fits, want 1: %+v", len(fits), fits)
	}
	if fits[0].Conflict == "" {
		t.Fatalf("one and four per point agreed with each other: %+v", fits[0])
	}
	if fits[0].Err() == nil {
		t.Fatal("a disputed value can be stored by accident")
	}
}

// Taking a point out measures the same thing as putting one in.
func TestRemovingAPointMeasuresTheSameValue(t *testing.T) {
	fits := FitTalents([]Snapshot{
		snapWith(1, "00500", 15, oneItem),
		snapWith(2, "00000", 10, oneItem),
	})
	if len(fits) != 1 || fits[0].PerPoint != 1 {
		t.Fatalf("a respec measures the same value: %+v", fits)
	}
}

// A talent string this does not understand is refused rather than guessed at,
// so a change to the addon's format cannot be read as a change to the game.
func TestAnUnreadableTalentStringIsRefused(t *testing.T) {
	for _, bad := range []string{"0x000", "", "   "} {
		if _, ok := talentsOf(Snapshot{Talents: bad}); ok {
			t.Errorf("%q was read as talents", bad)
		}
	}
	if _, ok := talentsOf(Snapshot{Talents: "00500"}); !ok {
		t.Fatal("a plain talent string was refused")
	}
}

// A client that keeps a tree of nodes names each talent by its node, and that
// is preferred: a node names the talent, a position only names where it sat.
func TestTalentsAreReadFromNodesWhenTheClientHasThem(t *testing.T) {
	a := snapWith(1, "00000", 10, oneItem)
	a.TalentRanks = map[string]int{"105939": 0}
	b := snapWith(2, "00000", 15, oneItem)
	b.TalentRanks = map[string]int{"105939": 5}
	fits := FitTalents([]Snapshot{a, b})
	if len(fits) != 1 {
		t.Fatalf("got %d fits, want 1: %+v", len(fits), fits)
	}
	if fits[0].Key != "node:105939" || fits[0].PerPoint != 1 {
		t.Fatalf("want one crit per point on node 105939: %+v", fits[0])
	}
	// The node reading wins even though the string says nothing changed.
	if fits[0].Stat != "critChance" {
		t.Fatalf("wrong stat: %+v", fits[0])
	}
}

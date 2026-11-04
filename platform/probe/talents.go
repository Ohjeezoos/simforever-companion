package probe

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// The client says what a talent does but never how that scales with the points
// spent on it. Forever collapsed each talent into one spell with a maximum
// number of ranks, and no column anywhere states whether the number on it is
// what one point buys or what the whole talent is worth. The two readings
// differ by five times on some talents, which is the difference between a
// simulation worth trusting and one that is not.
//
// The game itself answers. A character who spends a point sees a number on the
// character sheet move, and the addon snapshots the sheet whenever talents
// change. Two snapshots that differ in one talent and nothing else measure
// that talent outright.

// KindTalentPerPoint is the measurement kind for what one point in one talent
// is worth, on one stat.
const KindTalentPerPoint = "talent_per_point"

// TalentChange is one talent whose points differ between two snapshots. Key
// names the talent the way the client stated it: "node:105939" where the
// client keeps a tree of nodes, or "1/3" where it counts tabs and positions.
type TalentChange struct {
	Key  string `json:"key"`
	From int    `json:"from"`
	To   int    `json:"to"`
}

// TalentReading is one measurement: a talent moved by some points, and a stat
// moved with it.
type TalentReading struct {
	Key      string  `json:"key"`
	From     int     `json:"from"`
	To       int     `json:"to"`
	Stat     string  `json:"stat"`
	Before   float64 `json:"before"`
	After    float64 `json:"after"`
	PerPoint float64 `json:"perPoint"`
	Level    int32   `json:"level"`
	At       int64   `json:"at"`
}

// TalentFit is what one point in one talent is worth on one stat, across every
// time it was seen.
type TalentFit struct {
	Key      string  `json:"key"`
	Stat     string  `json:"stat"`
	PerPoint float64 `json:"perPoint"`
	Min      float64 `json:"min"`
	Max      float64 `json:"max"`
	N        int     `json:"n"`
	// Conflict is set when readings disagree. What one point buys is a
	// constant, so disagreement is a fault to report rather than something to
	// average away - the same rule the rating fits follow.
	Conflict string          `json:"conflict,omitempty"`
	Readings []TalentReading `json:"readings"`
}

// Err reports a conflict as an error, so a disputed value cannot be stored by
// accident.
func (f TalentFit) Err() error {
	if f.Conflict == "" {
		return nil
	}
	return fmt.Errorf("%w: talent %s on %s: %s", ErrRatingConflict, f.Key, f.Stat, f.Conflict)
}

// talentPoints reads a talent string into points per tab. It returns false for
// anything that is not the shape the addon writes, so a changed format is
// noticed rather than misread.
func talentPoints(s string) ([][]int, bool) {
	if strings.TrimSpace(s) == "" {
		return nil, false
	}
	var out [][]int
	for _, tab := range strings.Split(s, "-") {
		row := make([]int, 0, len(tab))
		for _, r := range tab {
			if r < '0' || r > '9' {
				return nil, false
			}
			row = append(row, int(r-'0'))
		}
		out = append(out, row)
	}
	return out, true
}

// talentsOf reads one snapshot's talents into points by talent key, whichever
// way the client stated them. It returns false when the snapshot says nothing
// about talents, which is not the same as saying nobody spent any.
func talentsOf(s Snapshot) (map[string]int, bool) {
	if len(s.TalentRanks) > 0 {
		out := make(map[string]int, len(s.TalentRanks))
		for node, rank := range s.TalentRanks {
			out["node:"+node] = rank
		}
		return out, true
	}
	tabs, ok := talentPoints(s.Talents)
	if !ok {
		return nil, false
	}
	out := map[string]int{}
	for tab := range tabs {
		for i, points := range tabs[tab] {
			out[fmt.Sprintf("%d/%d", tab+1, i+1)] = points
		}
	}
	return out, true
}

// TalentDiff names every talent whose points differ. It reports false when the
// two cannot be compared at all, because then nothing about them can be read.
func TalentDiff(before, after map[string]int) ([]TalentChange, bool) {
	if before == nil || after == nil {
		return nil, false
	}
	keys := map[string]bool{}
	for k := range before {
		keys[k] = true
	}
	for k := range after {
		keys[k] = true
	}
	var out []TalentChange
	for k := range keys {
		if before[k] != after[k] {
			out = append(out, TalentChange{Key: k, From: before[k], To: after[k]})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, true
}

// sheet is every number on the character sheet this can attribute to a talent,
// by the name it is reported under.
func sheet(s Snapshot) map[string]float64 {
	return map[string]float64{
		"critChance":        s.Combat.CritChance,
		"rangedCritChance":  s.Combat.RangedCritChance,
		"spellCritChance":   s.Combat.SpellCritChance,
		"hitModifier":       s.Combat.HitModifier,
		"spellHitModifier":  s.Combat.SpellHitModifier,
		"attackPower":       s.Combat.AttackPower,
		"rangedAttackPower": s.Combat.RangedAttackPower,
		"meleeHaste":        s.Combat.MeleeHaste,
		"expertise":         s.Combat.Expertise,
		"dodge":             s.Combat.Dodge,
		"parry":             s.Combat.Parry,
		"block":             s.Combat.Block,
		"defense":           s.Combat.Defense,
		"armor":             s.Combat.Armor,
		"strength":          s.Stats.Strength,
		"agility":           s.Stats.Agility,
		"stamina":           s.Stats.Stamina,
		"intellect":         s.Stats.Intellect,
		"spirit":            s.Stats.Spirit,
	}
}

// sameGear says whether two snapshots were taken wearing the same things. A
// stat that moved because an item changed is not a talent's doing.
func sameGear(a, b Snapshot) bool {
	if len(a.Gear) != len(b.Gear) {
		return false
	}
	key := func(s Snapshot) []string {
		out := make([]string, 0, len(s.Gear))
		for _, g := range s.Gear {
			out = append(out, fmt.Sprintf("%d:%s", g.Slot, g.ItemString))
		}
		sort.Strings(out)
		return out
	}
	ka, kb := key(a), key(b)
	for i := range ka {
		if ka[i] != kb[i] {
			return false
		}
	}
	return true
}

// talentEpsilon is how much a stat may move and still count as unmoved. The
// client reports percentages as floats and rounds them on the way out.
const talentEpsilon = 0.005

// FitTalents reads a character's snapshots and measures what one point in a
// talent is worth. It only compares snapshots that differ in exactly one
// talent, at the same level, in the same gear: anything else leaves more than
// one reason for a number to have moved, and a measurement with two possible
// causes is not a measurement.
func FitTalents(snaps []Snapshot) []TalentFit {
	ordered := make([]Snapshot, len(snaps))
	copy(ordered, snaps)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].ID < ordered[j].ID })

	byKey := map[string]*TalentFit{}
	for i := 1; i < len(ordered); i++ {
		a, b := ordered[i-1], ordered[i]
		if a.Level != b.Level || !sameGear(a, b) {
			continue
		}
		ta, okA := talentsOf(a)
		tb, okB := talentsOf(b)
		if !okA || !okB {
			continue
		}
		changes, ok := TalentDiff(ta, tb)
		if !ok || len(changes) != 1 {
			continue
		}
		c := changes[0]
		points := c.To - c.From
		if points == 0 {
			continue
		}
		before, after := sheet(a), sheet(b)
		for stat, was := range before {
			now := after[stat]
			if math.Abs(now-was) < talentEpsilon {
				continue
			}
			r := TalentReading{
				Key: c.Key, From: c.From, To: c.To,
				Stat: stat, Before: was, After: now,
				PerPoint: (now - was) / float64(points),
				Level:    b.Level, At: b.At,
			}
			key := c.Key + "/" + stat
			f := byKey[key]
			if f == nil {
				f = &TalentFit{Key: c.Key, Stat: stat,
					Min: r.PerPoint, Max: r.PerPoint}
				byKey[key] = f
			}
			f.Readings = append(f.Readings, r)
			f.N++
			f.Min = math.Min(f.Min, r.PerPoint)
			f.Max = math.Max(f.Max, r.PerPoint)
			f.PerPoint = f.Readings[0].PerPoint
		}
	}

	out := make([]TalentFit, 0, len(byKey))
	for _, f := range byKey {
		if f.Max-f.Min > talentEpsilon {
			f.Conflict = fmt.Sprintf("readings range from %g to %g per point", f.Min, f.Max)
		}
		out = append(out, *f)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Key != out[j].Key {
			return out[i].Key < out[j].Key
		}
		return out[i].Stat < out[j].Stat
	})
	return out
}

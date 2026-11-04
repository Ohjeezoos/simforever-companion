package probe

import (
	"sort"

	"github.com/Ohjeezoos/simforever-companion/platform/ingest"
	"github.com/Ohjeezoos/simforever-companion/platform/ingest/fit"
)

// spellSchools are the combat log school bits, in the order the addon records
// GetSpellBonusDamage under.
var spellSchools = []struct {
	bit  int32
	name string
}{
	{1, "physical"}, {2, "holy"}, {4, "fire"}, {8, "nature"}, {16, "frost"}, {32, "shadow"}, {64, "arcane"},
}

// schoolPower picks the spell power behind a hit. A multi-school spell uses
// the highest of its schools, which is how the client pays them out.
func schoolPower(mask int32, sp map[string]float64) (float64, bool) {
	if len(sp) == 0 {
		return 0, false
	}
	best, found := 0.0, false
	for _, s := range spellSchools {
		if mask&s.bit == 0 {
			continue
		}
		if v, ok := sp[s.name]; ok {
			found = true
			if v > best {
				best = v
			}
		}
	}
	return best, found
}

// BuildResult turns the addon's own combat log into the shape the log ingest
// fitter already understands: one fight, the player's events, and the spell
// power of the snapshot that was current on each event carried per event, the
// way advanced combat logging would carry it.
func BuildResult(exp *Export, source string) *ingest.Result {
	guid := exp.Character.GUID
	if guid == "" {
		guid = "Player-probe"
	}
	snaps := exp.SnapshotByID()
	f := ingest.Fight{EncounterName: source,
		Players: []ingest.Player{{GUID: guid, Name: exp.Character.Name, Class: exp.Character.Class, Level: exp.Character.Level}}}
	var first float64
	for i := range exp.Events {
		e := &exp.Events[i]
		if e.Kind == "" || e.Kind == "fight_end" {
			continue
		}
		if first == 0 {
			first = e.Time
		}
		ev := ingest.Event{
			Ms:         int64((e.Time - first) * 1000),
			Event:      e.Kind,
			SourceGUID: guid,
			SourceName: exp.Character.Name,
			TargetGUID: e.Target,
			SpellID:    e.Spell,
			SpellName:  e.SpellName,
			School:     e.School,
			Amount:     int64(e.Amount),
			Overkill:   int64(e.Overkill),
			Resisted:   int64(e.Resisted),
			Blocked:    int64(e.Blocked),
			Absorbed:   int64(e.Absorbed),
			Critical:   bool(e.Crit),
			Glancing:   bool(e.Glancing),
			Crushing:   bool(e.Crushing),
		}
		if t := exp.Targets[e.Target]; t.Name != "" {
			ev.TargetName = t.Name
		}
		if snap := snaps[e.Snapshot]; snap != nil {
			if power, ok := schoolPower(e.School, snap.SpellPower); ok {
				ev.Extra = map[string]any{"advGuid": guid, "spellPower": power, "snapshot": e.Snapshot}
			}
		}
		if ev.Ms > f.DurationMs {
			f.DurationMs = ev.Ms
		}
		f.Events = append(f.Events, ev)
	}
	return &ingest.Result{Fights: []ingest.Fight{f}, Lines: int64(len(exp.Events))}
}

// CoefFit is one spell's fitted spell power coefficient, in the same pair of
// measurements the log fitting harness produces.
type CoefFit struct {
	SpellID     int32           `json:"spellId"`
	SpellName   string          `json:"spellName"`
	Samples     int             `json:"samples"`
	Coefficient fit.Measurement `json:"coefficient"`
	Base        fit.Measurement `json:"baseDamage"`
}

// CoefSkip records a spell that could not be fitted, with the reason, so an
// empty result is never mistaken for a zero coefficient.
type CoefSkip struct {
	SpellID   int32  `json:"spellId"`
	SpellName string `json:"spellName"`
	Samples   int    `json:"samples"`
	Reason    string `json:"reason"`
}

// FitCoefficients fits every spell with clean direct damage in the export.
func FitCoefficients(exp *Export, opts fit.CoefOptions, source string) ([]CoefFit, []CoefSkip) {
	res := BuildResult(exp, source)
	names := map[int32]string{}
	var ids []int32
	for i := range exp.Events {
		e := &exp.Events[i]
		if e.Kind != "SPELL_DAMAGE" || e.Spell == 0 {
			continue
		}
		if _, seen := names[e.Spell]; !seen {
			ids = append(ids, e.Spell)
		}
		if e.SpellName != "" {
			names[e.Spell] = e.SpellName
		} else if _, seen := names[e.Spell]; !seen {
			names[e.Spell] = ""
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	fits, skipped := []CoefFit{}, []CoefSkip{}
	for _, id := range ids {
		samples := fit.CollectSamples(res, id, source)
		coef, base, err := fit.Coefficient(id, samples, opts)
		if err != nil {
			skipped = append(skipped, CoefSkip{SpellID: id, SpellName: names[id], Samples: len(samples), Reason: err.Error()})
			continue
		}
		fits = append(fits, CoefFit{SpellID: id, SpellName: names[id], Samples: len(samples), Coefficient: coef, Base: base})
	}
	return fits, skipped
}

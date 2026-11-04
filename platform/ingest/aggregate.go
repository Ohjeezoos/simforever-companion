package ingest

import (
	"sort"
	"strings"
	"time"
)

type SpellDamage struct {
	SpellID   int32          `json:"spellId"`
	SpellName string         `json:"spellName"`
	Hits      int            `json:"hits"`
	Ticks     int            `json:"ticks"`
	Crits     int            `json:"crits"`
	Total     int64          `json:"total"`
	Min       int64          `json:"min"`
	Max       int64          `json:"max"`
	Mean      float64        `json:"mean"`
	Casts     int            `json:"casts"`
	Misses    map[string]int `json:"misses,omitempty"`
}

type Cast struct {
	Ms         int64     `json:"tsMs"`
	Time       time.Time `json:"ts"`
	Event      string    `json:"event"`
	SpellID    int32     `json:"spellId"`
	SpellName  string    `json:"spellName"`
	TargetGUID string    `json:"targetGuid,omitempty"`
	TargetName string    `json:"targetName,omitempty"`
}

type AuraUptime struct {
	SpellID   int32   `json:"spellId"`
	SpellName string  `json:"spellName"`
	UptimeMs  int64   `json:"uptimeMs"`
	Fraction  float64 `json:"fraction"`
	Applies   int     `json:"applies"`
	Refreshes int     `json:"refreshes"`
}

type PlayerSummary struct {
	Player
	Damage int64         `json:"damage"`
	DPS    float64       `json:"dps"`
	Spells []SpellDamage `json:"spells"`
	Casts  int           `json:"casts"`
}

type FightSummary struct {
	EncounterID   int32           `json:"encounterId"`
	EncounterName string          `json:"encounterName"`
	StartedAt     time.Time       `json:"startedAt"`
	DurationMs    int64           `json:"durationMs"`
	Success       *bool           `json:"success,omitempty"`
	Events        int             `json:"events"`
	Players       []PlayerSummary `json:"players"`
	Warnings      []string        `json:"warnings,omitempty"`
}

// DamageBySpell aggregates damage dealt by sourceGUID, sorted by total.
// Swings are reported under spell id 0 with the name "Melee".
func DamageBySpell(f *Fight, sourceGUID string) []SpellDamage {
	by := map[int32]*SpellDamage{}
	get := func(ev *Event) *SpellDamage {
		id, name := ev.SpellID, ev.SpellName
		if strings.HasPrefix(ev.Event, "SWING_") {
			id, name = 0, "Melee"
		}
		d := by[id]
		if d == nil {
			d = &SpellDamage{SpellID: id, SpellName: name}
			by[id] = d
		}
		return d
	}
	for i := range f.Events {
		ev := &f.Events[i]
		if ev.SourceGUID != sourceGUID {
			continue
		}
		switch {
		case isDamage(ev.Event):
			d := get(ev)
			if isPeriodic(ev.Event) {
				d.Ticks++
			} else {
				d.Hits++
			}
			if ev.Critical {
				d.Crits++
			}
			d.Total += ev.Amount
			if d.Hits+d.Ticks == 1 || ev.Amount < d.Min {
				d.Min = ev.Amount
			}
			if ev.Amount > d.Max {
				d.Max = ev.Amount
			}
		case strings.HasSuffix(ev.Event, "_MISSED"):
			d := get(ev)
			if d.Misses == nil {
				d.Misses = map[string]int{}
			}
			d.Misses[ev.extraStr("missType")]++
		case ev.Event == "SPELL_CAST_SUCCESS":
			get(ev).Casts++
		}
	}
	out := make([]SpellDamage, 0, len(by))
	for _, d := range by {
		if n := d.Hits + d.Ticks; n > 0 {
			d.Mean = float64(d.Total) / float64(n)
		}
		out = append(out, *d)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Total != out[j].Total {
			return out[i].Total > out[j].Total
		}
		return out[i].SpellID < out[j].SpellID
	})
	return out
}

func TotalDamage(f *Fight, sourceGUID string) int64 {
	var total int64
	for i := range f.Events {
		ev := &f.Events[i]
		if ev.SourceGUID == sourceGUID && isDamage(ev.Event) {
			total += ev.Amount
		}
	}
	return total
}

func DPS(f *Fight, sourceGUID string) float64 {
	if f.DurationMs <= 0 {
		return 0
	}
	return float64(TotalDamage(f, sourceGUID)) / (float64(f.DurationMs) / 1000)
}

// CastSequence lists SPELL_CAST_START and SPELL_CAST_SUCCESS events by
// sourceGUID in log order. Filter on Event for successful casts only.
func CastSequence(f *Fight, sourceGUID string) []Cast {
	var out []Cast
	for i := range f.Events {
		ev := &f.Events[i]
		if ev.SourceGUID != sourceGUID || !isCast(ev.Event) {
			continue
		}
		out = append(out, Cast{Ms: ev.Ms, Time: ev.Time, Event: ev.Event, SpellID: ev.SpellID, SpellName: ev.SpellName, TargetGUID: ev.TargetGUID, TargetName: ev.TargetName})
	}
	return out
}

// Uptime measures how long aura spellID was on targetGUID during the fight.
// An aura removed before any apply is assumed present from the fight start,
// one never removed is assumed present until the fight end.
func Uptime(f *Fight, targetGUID string, spellID int32) AuraUptime {
	u := AuraUptime{SpellID: spellID}
	var start int64 = -1
	end := f.DurationMs
	for i := range f.Events {
		ev := &f.Events[i]
		if ev.TargetGUID != targetGUID || ev.SpellID != spellID {
			continue
		}
		switch ev.Event {
		case "SPELL_AURA_APPLIED":
			u.SpellName = ev.SpellName
			u.Applies++
			if start < 0 {
				start = ev.Ms
			}
		case "SPELL_AURA_REFRESH", "SPELL_AURA_APPLIED_DOSE":
			u.SpellName = ev.SpellName
			u.Refreshes++
			if start < 0 {
				start = 0
			}
		case "SPELL_AURA_REMOVED":
			u.SpellName = ev.SpellName
			if start < 0 {
				start = 0
			}
			u.UptimeMs += ev.Ms - start
			start = -1
		}
	}
	if start >= 0 {
		u.UptimeMs += end - start
	}
	if end > 0 {
		u.Fraction = float64(u.UptimeMs) / float64(end)
	}
	return u
}

func Summarize(f *Fight) FightSummary {
	s := FightSummary{EncounterID: f.EncounterID, EncounterName: f.EncounterName, StartedAt: f.StartedAt, DurationMs: f.DurationMs, Success: f.Success, Events: len(f.Events), Warnings: f.Warnings}
	for _, p := range f.Players {
		ps := PlayerSummary{Player: p, Spells: DamageBySpell(f, p.GUID)}
		ps.Damage = TotalDamage(f, p.GUID)
		ps.DPS = DPS(f, p.GUID)
		for _, c := range CastSequence(f, p.GUID) {
			if c.Event == "SPELL_CAST_SUCCESS" {
				ps.Casts++
			}
		}
		s.Players = append(s.Players, ps)
	}
	return s
}

package ingest

import (
	"sort"
	"strings"
	"time"
)

const (
	flagHostile = 0x40
	flagPlayer  = 0x400
	lookback    = 5 * time.Second
)

type combatant struct {
	guid, name, class string
	level             int64
	classes           map[string]bool
	stats             []statsAt
}

type statsAt struct {
	at    time.Time
	stats map[string]any
}

type segmenter struct {
	gap         time.Duration
	fights      []Fight
	cur         *Fight
	inEncounter bool
	lastHostile time.Time
	dmgByTarget map[string]int64
	recent      []Event
	units       map[string]*combatant
}

func newSegmenter(gap time.Duration) *segmenter {
	return &segmenter{gap: gap, units: map[string]*combatant{}}
}

func isPlayerGUID(guid string) bool { return strings.HasPrefix(guid, "Player-") }

func (ev *Event) hostile() bool {
	if ev.SourceFlags&flagHostile != 0 || ev.TargetFlags&flagHostile != 0 {
		return true
	}
	if ev.SourceFlags == 0 && ev.TargetFlags == 0 {
		return strings.HasPrefix(ev.TargetGUID, "Creature-") || strings.HasPrefix(ev.SourceGUID, "Creature-")
	}
	return false
}

func (ev *Event) combat() bool {
	return isDamage(ev.Event) || strings.HasSuffix(ev.Event, "_MISSED")
}

func (s *segmenter) unit(guid, name string) *combatant {
	u := s.units[guid]
	if u == nil {
		u = &combatant{guid: guid, classes: map[string]bool{}}
		s.units[guid] = u
	}
	if name != "" && u.name == "" {
		u.name = name
	}
	return u
}

func (s *segmenter) observe(ev *Event) {
	if isPlayerGUID(ev.SourceGUID) {
		u := s.unit(ev.SourceGUID, ev.SourceName)
		if isCast(ev.Event) {
			if c := classBySpell[ev.SpellName]; c != "" {
				u.classes[c] = true
			}
		}
	}
	if isPlayerGUID(ev.TargetGUID) {
		s.unit(ev.TargetGUID, ev.TargetName)
	}
	// The level on a line belongs to whoever the advanced block describes,
	// which is the attacker on some events and the thing it hit on others.
	// Creatures were being thrown away: without their level nothing about a
	// fight can be read against the level difference it was fought at, and
	// that difference is what decides how often an attack lands at all.
	if ev.Extra == nil {
		return
	}
	guid := ev.extraStr("advGuid")
	if guid == "" {
		return
	}
	name := ""
	switch guid {
	case ev.SourceGUID:
		name = ev.SourceName
	case ev.TargetGUID:
		name = ev.TargetName
	default:
		return
	}
	// The last field of the advanced block is the unit's level when the
	// block describes a creature. When it describes a player it is some
	// other number: across 574 lines of one log it stayed 2 while the
	// addon recorded that character going from level 11 to level 12. Taking
	// it for a player's level put every swing that player made into a level
	// difference nine steps from the one it was fought at.
	if lvl := ev.extraInt("level"); lvl > 0 && !isPlayerGUID(guid) {
		s.unit(guid, name).level = lvl
	}
}

func (s *segmenter) add(ev Event, warn func(string)) {
	switch ev.Event {
	case "COMBATANT_INFO":
		u := s.unit(ev.SourceGUID, "")
		stats, _ := ev.Extra["stats"].(map[string]any)
		u.stats = append(u.stats, statsAt{ev.Time, stats})
		return
	case "ENCOUNTER_START":
		s.close(s.lastHostile)
		s.open(ev.Time, nil)
		s.inEncounter = true
		s.cur.EncounterID = int32(ev.extraInt("encounterId"))
		s.cur.EncounterName = ev.extraStr("name")
		s.cur.Events = append(s.cur.Events, ev)
		return
	case "ENCOUNTER_END":
		if s.cur == nil || !s.inEncounter {
			warn("ENCOUNTER_END without ENCOUNTER_START")
			return
		}
		if v, ok := ev.Extra["success"].(bool); ok {
			s.cur.Success = &v
		}
		s.cur.Events = append(s.cur.Events, ev)
		s.close(ev.Time)
		return
	}
	s.observe(&ev)
	hostile := ev.combat() && ev.hostile()
	if s.cur != nil && !s.inEncounter && hostile && ev.Time.Sub(s.lastHostile) >= s.gap {
		s.close(s.lastHostile)
	}
	if s.cur == nil {
		if !hostile || !isDamage(ev.Event) {
			s.remember(ev)
			return
		}
		s.open(ev.Time, s.pullRecent(ev.Time))
	}
	s.cur.Events = append(s.cur.Events, ev)
	if hostile {
		s.lastHostile = ev.Time
		if isDamage(ev.Event) && (ev.SourceFlags&flagPlayer != 0 || isPlayerGUID(ev.SourceGUID)) {
			s.dmgByTarget[ev.TargetGUID] += ev.Amount
		}
	}
	if ev.Event == "UNIT_DIED" && !s.inEncounter && ev.TargetGUID == s.mainTarget() {
		s.close(ev.Time)
	}
}

func (s *segmenter) remember(ev Event) {
	s.recent = append(s.recent, ev)
	if len(s.recent) > 512 {
		s.recent = s.recent[256:]
	}
}

// pullRecent returns player casts from the lookback window so the opener that
// produced the first damage event is part of the fight.
func (s *segmenter) pullRecent(at time.Time) []Event {
	var out []Event
	for _, e := range s.recent {
		if isCast(e.Event) && isPlayerGUID(e.SourceGUID) && at.Sub(e.Time) <= lookback {
			out = append(out, e)
		}
	}
	s.recent = s.recent[:0]
	return out
}

func (s *segmenter) open(at time.Time, pre []Event) {
	s.cur = &Fight{StartedAt: at}
	if len(pre) > 0 && pre[0].Time.Before(at) {
		s.cur.StartedAt = pre[0].Time
	}
	s.cur.Events = append(s.cur.Events, pre...)
	s.inEncounter = false
	s.lastHostile = at
	s.dmgByTarget = map[string]int64{}
}

func (s *segmenter) mainTarget() string {
	best, bestDmg := "", int64(-1)
	for g, d := range s.dmgByTarget {
		if d > bestDmg || d == bestDmg && g < best {
			best, bestDmg = g, d
		}
	}
	return best
}

func (s *segmenter) close(at time.Time) {
	f := s.cur
	if f == nil {
		return
	}
	s.cur = nil
	s.inEncounter = false
	if at.Before(f.StartedAt) {
		at = f.StartedAt
	}
	f.EndedAt = at
	f.DurationMs = at.Sub(f.StartedAt).Milliseconds()
	for i := range f.Events {
		f.Events[i].Ms = f.Events[i].Time.Sub(f.StartedAt).Milliseconds()
	}
	s.fights = append(s.fights, *f)
}

// levels is every unit the log said the level of, which is creatures only:
// a player's level is never stated on any line.
func (s *segmenter) levels() map[string]int32 {
	out := map[string]int32{}
	for guid, u := range s.units {
		if u.level > 0 {
			out[guid] = int32(u.level)
		}
	}
	return out
}

func (s *segmenter) finish(warn func(string)) []Fight {
	if s.cur != nil {
		if s.inEncounter {
			warn("ENCOUNTER_START without ENCOUNTER_END at end of log")
			s.cur.Warnings = append(s.cur.Warnings, "encounter not closed before end of log")
		}
		s.close(s.lastHostile)
	}
	for i := range s.fights {
		s.fights[i].Players = s.players(&s.fights[i])
	}
	return s.fights
}

func (s *segmenter) players(f *Fight) []Player {
	seen := map[string]bool{}
	var out []Player
	for _, ev := range f.Events {
		for _, guid := range []string{ev.SourceGUID, ev.TargetGUID} {
			if !isPlayerGUID(guid) || seen[guid] {
				continue
			}
			seen[guid] = true
			out = append(out, s.player(guid, f.StartedAt))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].GUID < out[j].GUID })
	return out
}

func (s *segmenter) player(guid string, at time.Time) Player {
	u := s.unit(guid, "")
	p := Player{GUID: guid, Name: u.name, Level: int32(u.level)}
	for _, st := range u.stats {
		if !st.at.After(at) {
			p.Stats = st.stats
		}
	}
	if p.Stats != nil {
		if spec, ok := p.Stats["specId"].(int64); ok {
			p.Class = classBySpec[spec]
		}
	}
	if p.Class == "" && len(u.classes) == 1 {
		for c := range u.classes {
			p.Class = c
		}
	}
	return p
}

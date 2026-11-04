// Package ingest parses WoW combat logs (WoWCombatLog.txt) into fights, players
// and events, and fits spell coefficients from them. Contract types live here;
// fields may be added, never renamed or removed.
package ingest

import (
	"errors"
	"time"
)

type Event struct {
	Time       time.Time      `json:"ts"`
	Ms         int64          `json:"tsMs"` // milliseconds since fight start
	Event      string         `json:"event"`
	SourceGUID string         `json:"sourceGuid"`
	SourceName string         `json:"sourceName"`
	TargetGUID string         `json:"targetGuid"`
	TargetName string         `json:"targetName"`
	SpellID    int32          `json:"spellId"`
	SpellName  string         `json:"spellName"`
	School     int32          `json:"school"`
	Amount     int64          `json:"amount"`
	Overkill   int64          `json:"overkill"`
	Resisted   int64          `json:"resisted"`
	Blocked    int64          `json:"blocked"`
	Absorbed   int64          `json:"absorbed"`
	Critical   bool           `json:"critical"`
	Glancing   bool           `json:"glancing"`
	Crushing   bool           `json:"crushing"`
	Extra      map[string]any `json:"extra,omitempty"`

	SourceFlags int64 `json:"sourceFlags,omitempty"`
	TargetFlags int64 `json:"targetFlags,omitempty"`
}

type Player struct {
	GUID  string         `json:"guid"`
	Name  string         `json:"name"`
	Class string         `json:"class"`
	Spec  string         `json:"spec,omitempty"`
	Stats map[string]any `json:"stats,omitempty"` // from COMBATANT_INFO when present
	// Level is only ever stated for a creature; no line states a player's.
	Level int32 `json:"level,omitempty"`
}

type Fight struct {
	EncounterID   int32     `json:"encounterId"`
	EncounterName string    `json:"encounterName"`
	StartedAt     time.Time `json:"startedAt"`
	EndedAt       time.Time `json:"endedAt"`
	DurationMs    int64     `json:"durationMs"`
	Success       *bool     `json:"success,omitempty"`
	Players       []Player  `json:"players"`
	Events        []Event   `json:"events"`
	Warnings      []string  `json:"warnings,omitempty"`
}

type Result struct {
	Fights   []Fight  `json:"fights"`
	Warnings []string `json:"warnings,omitempty"`
	Lines    int64    `json:"lines"`
	// DamageLines counts the damage and miss events in the file, whether or
	// not any of them ended up in a fight. A file full of them and empty of
	// fights is one whose contents were discarded.
	DamageLines int `json:"damageLines"`
	// Levels is every unit whose level the log stated, by GUID, which is
	// creatures only: what a player's line states is the level of its gear.
	// A creature never says its level on the line where it was missed, so
	// the level has to be remembered from the lines where it did say.
	Levels map[string]int32 `json:"-"`
}

var ErrNotImplemented = errors.New("ingest: not implemented yet")

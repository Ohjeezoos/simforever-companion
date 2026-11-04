// Package probe reads the SimforeverProbe addon's saved variables or JSON
// export and turns the client's own numbers into platform measurements: the
// rating to percent conversion Forever does not ship in its client database,
// a check of the converted item stats, and spell power coefficients.
package probe

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// SavedVariable is the global the addon declares in its .toc.
const SavedVariable = "SimforeverProbeDB"

// Bool is a combat log flag, which the client reports as true, as 1 or not at all.
type Bool bool

func (b *Bool) UnmarshalJSON(p []byte) error {
	switch s := string(bytes.TrimSpace(p)); s {
	case "", "null", "false", "0":
		*b = false
	case "true":
		*b = true
	default:
		var f float64
		if json.Unmarshal(p, &f) == nil {
			*b = f != 0
			return nil
		}
		var str string
		if json.Unmarshal(p, &str) == nil {
			*b = str != "" && str != "0" && str != "false"
			return nil
		}
		*b = true
	}
	return nil
}

type Build struct {
	Version string `json:"version"`
	Build   string `json:"build"`
	TOC     int32  `json:"toc"`
}

// String is the full client version, the form the builds table stores.
func (b Build) String() string {
	switch {
	case b.Version == "":
		return b.Build
	case b.Build == "":
		return b.Version
	}
	return b.Version + "." + b.Build
}

type Character struct {
	Name       string `json:"name"`
	Realm      string `json:"realm"`
	GUID       string `json:"guid"`
	Class      string `json:"class"`
	ClassName  string `json:"className"`
	Race       string `json:"race"`
	RaceName   string `json:"raceName"`
	Level      int32  `json:"level"`
	Faction    string `json:"faction"`
	Talents    string `json:"talents"`
	Build      Build  `json:"build"`
	RecordedAt int64  `json:"recordedAt"`
}

type PrimaryStats struct {
	Strength  float64 `json:"strength"`
	Agility   float64 `json:"agility"`
	Stamina   float64 `json:"stamina"`
	Intellect float64 `json:"intellect"`
	Spirit    float64 `json:"spirit"`
	Health    float64 `json:"health"`
	Power     float64 `json:"power"`
	ManaRegen float64 `json:"manaRegen"`
	// The client states a stat four ways and only the effective one is the
	// character's total. What a class is worth at a level before anything was
	// cast on it is the base, and no table in this client carries it, so a
	// level below the maximum can only be modelled from these.
	StrengthBase  float64 `json:"strengthBase"`
	AgilityBase   float64 `json:"agilityBase"`
	StaminaBase   float64 `json:"staminaBase"`
	IntellectBase float64 `json:"intellectBase"`
	SpiritBase    float64 `json:"spiritBase"`
}

type CombatStats struct {
	AttackPower       float64 `json:"attackPower"`
	RangedAttackPower float64 `json:"rangedAttackPower"`
	MainHandSpeed     float64 `json:"mainHandSpeed"`
	OffHandSpeed      float64 `json:"offHandSpeed"`
	MainHandMin       float64 `json:"mainHandMin"`
	MainHandMax       float64 `json:"mainHandMax"`
	OffHandMin        float64 `json:"offHandMin"`
	OffHandMax        float64 `json:"offHandMax"`
	CritChance        float64 `json:"critChance"`
	RangedCritChance  float64 `json:"rangedCritChance"`
	SpellCritChance   float64 `json:"spellCritChance"`
	HitModifier       float64 `json:"hitModifier"`
	SpellHitModifier  float64 `json:"spellHitModifier"`
	MeleeHaste        float64 `json:"meleeHaste"`
	SpellHaste        float64 `json:"spellHaste"`
	Expertise         float64 `json:"expertise"`
	Armor             float64 `json:"armor"`
	Defense           float64 `json:"defense"`
	Dodge             float64 `json:"dodge"`
	Parry             float64 `json:"parry"`
	Block             float64 `json:"block"`
}

// SnapshotRating is one rating amount with the percent the client says it is
// worth at that moment. The pair is the measurement.
type SnapshotRating struct {
	Rating  float64 `json:"rating"`
	Percent float64 `json:"percent"`
}

type GearPiece struct {
	Slot       int32   `json:"slot"`
	ID         int32   `json:"id"`
	Link       string  `json:"link"`
	ItemString string  `json:"itemString"`
	Level      float64 `json:"level"`
}

type Snapshot struct {
	ID         int32                     `json:"id"`
	At         int64                     `json:"at"`
	Reason     string                    `json:"reason"`
	Level      int32                     `json:"level"`
	Zone       string                    `json:"zone"`
	Stats      PrimaryStats              `json:"stats"`
	Combat     CombatStats               `json:"combat"`
	SpellPower map[string]float64        `json:"spellPower"`
	Ratings    map[string]SnapshotRating `json:"ratings"`
	Gear       []GearPiece               `json:"gear"`
	// Skills is what the character sheet says each skill stands at, by name.
	// Weapon skill against the target's defence is what the whole melee hit
	// table is built on, and a character that has never swung the weapon it
	// holds is far below the cap of five times its level, so its swings say
	// nothing about that table until the skill is written down.
	Skills map[string]Skill `json:"skills,omitempty"`
	// Roster is everyone in the group and the level each one was. No line of a
	// combat log states a player's level, so without this only the character
	// running the addon has swings that can be put against a level difference
	// and everybody else in the dungeon is discarded.
	Roster  []RosterMember `json:"roster,omitempty"`
	Talents string         `json:"talents"`
	// TalentRanks is points per talent by the node the client keeps them
	// under, which is how a client built on a newer base states them. Where
	// it is present it is preferred: a node number names a talent outright,
	// where a position in a string only names where it sat on screen.
	TalentRanks map[string]int `json:"talentRanks,omitempty"`
}

// RosterMember is one person in the group at the moment of a snapshot.
type RosterMember struct {
	GUID      string `json:"guid"`
	Name      string `json:"name"`
	Level     int32  `json:"level"`
	Class     string `json:"class"`
	ClassName string `json:"className,omitempty"`
}

// Skill is one line of the character sheet's skill list.
type Skill struct {
	Rank int32 `json:"rank"`
	Max  int32 `json:"max,omitempty"`
	// Modifier is what a buff or an item adds on top of the rank.
	Modifier int32 `json:"modifier,omitempty"`
}

// Reading is one deduplicated rating amount, keyed by that amount in Export.Ratings.
type Reading struct {
	Percent float64 `json:"percent"`
	Level   int32   `json:"level"`
	At      int64   `json:"at"`
}

// Item is the client's own view of an item, GetItemStats included.
type Item struct {
	Name    string             `json:"name"`
	Quality int32              `json:"quality"`
	Ilvl    int32              `json:"ilvl"`
	Stats   map[string]float64 `json:"stats"`
	At      int64              `json:"at"`
}

// ItemID reads the item id out of an item string ("item:12640:0:...").
func ItemID(itemString string) (int32, bool) {
	parts := strings.Split(itemString, ":")
	if len(parts) < 2 || parts[0] != "item" {
		return 0, false
	}
	n, err := strconv.ParseInt(parts[1], 10, 32)
	if err != nil || n <= 0 {
		return 0, false
	}
	return int32(n), true
}

// Event is one of the player's own combat log lines, pointing at the snapshot
// that was current when it happened.
type Event struct {
	Kind         string  `json:"k"`
	Time         float64 `json:"t"`
	Snapshot     int32   `json:"s"`
	Target       string  `json:"d"`
	Spell        int32   `json:"spell"`
	SpellName    string  `json:"spellName"`
	School       int32   `json:"school"`
	Amount       float64 `json:"amount"`
	Overkill     float64 `json:"overkill"`
	Resisted     float64 `json:"resisted"`
	Blocked      float64 `json:"blocked"`
	Absorbed     float64 `json:"absorbed"`
	Crit         Bool    `json:"crit"`
	Glancing     Bool    `json:"glancing"`
	Crushing     Bool    `json:"crushing"`
	Miss         string  `json:"miss"`
	OffHand      Bool    `json:"offhand"`
	AmountMissed float64 `json:"amountMissed"`
	AuraType     string  `json:"auraType"`
	AuraAmount   float64 `json:"auraAmount"`
	PowerType    float64 `json:"powerType"`
	N            int32   `json:"n"`
	At           int64   `json:"at"`
}

type Target struct {
	NpcID          int32  `json:"npcID"`
	Name           string `json:"name"`
	Level          int32  `json:"level"`
	Classification string `json:"classification"`
}

// Export is both shapes the addon produces: the saved variables table and the
// JSON printed by /sfp export.
// SecretWhen says when a reading was withheld: in combat, at rest, or both.
type SecretWhen struct {
	Combat Bool `json:"combat"`
	Rest   Bool `json:"rest"`
}

// BlockedCall is an action this client refused to let the addon take. Named is
// false when the client did not say which function it was, and LastCalls is
// then the only evidence: the client calls the probe made just before.
type BlockedCall struct {
	At        float64  `json:"at"`
	By        string   `json:"by"`
	Fn        string   `json:"fn"`
	Named     Bool     `json:"named"`
	LastCalls []string `json:"lastCalls"`
}

type Export struct {
	Version   string                        `json:"version"`
	Build     Build                         `json:"build"`
	Character Character                     `json:"character"`
	Snapshots []Snapshot                    `json:"snapshots"`
	Events    []Event                       `json:"events"`
	Ratings   map[string]map[string]Reading `json:"ratings"`
	Items     map[string]Item               `json:"items"`
	Targets   map[string]Target             `json:"targets"`
	Notes     []string                      `json:"notes"`
	Recording bool                          `json:"recording"`
	// Secrets names the client functions whose values Forever hides from
	// addons, Blocked the ones it refuses to run for an addon at all. Both
	// decide what a probe can ever measure, so they travel with the export.
	Secrets map[string]map[string]SecretWhen `json:"secrets"`
	Blocked map[string]BlockedCall           `json:"blocked"`
	// ClientAPI names the secret-value helpers this client exposes to addons.
	// The values are a mix: a function is named by its type, a flag by being
	// true or false, so it is read loosely on purpose.
	ClientAPI map[string]any `json:"clientApi"`
	// ItemLevels is the level the game requires to wear each item, asked of the
	// client itself. ItemSparse leaves it at zero for 1934 of the items the
	// engine can build with, and zero there does not mean no requirement, so
	// this is the only thing that says what a level 20 may wear.
	ItemLevels    map[string]int32 `json:"itemLevels"`
	ItemLevelsRun *ItemLevelsRun   `json:"itemLevelsRun"`
}

// ItemLevelsRun records how much of the sweep the client actually answered, so
// a partial pass is visible as partial rather than read as the whole truth.
type ItemLevelsRun struct {
	At       int64 `json:"at"`
	Asked    int32 `json:"asked"`
	Answered int32 `json:"answered"`
	Missing  int32 `json:"missing"`
	// The addon used to write the build as a string and now writes the same
	// Build object it writes everywhere else. Reading only the string made two
	// snapshots unreadable, and a snapshot that will not parse is a session
	// thrown away, so both forms are accepted.
	Build BuildOrString `json:"build"`
}

// BuildOrString reads either "1.60.1.70009" or {"version":"1.60.1","build":"70009"}.
type BuildOrString struct{ Build }

func (b *BuildOrString) UnmarshalJSON(raw []byte) error {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		b.Build = Build{Version: s}
		return nil
	}
	return json.Unmarshal(raw, &b.Build)
}

// SnapshotByID indexes the snapshots an event can point at.
func (e *Export) SnapshotByID() map[int32]*Snapshot {
	out := make(map[int32]*Snapshot, len(e.Snapshots))
	for i := range e.Snapshots {
		out[e.Snapshots[i].ID] = &e.Snapshots[i]
	}
	return out
}

// Parse reads either the addon's SavedVariables Lua file or its JSON export.
func Parse(data []byte) (*Export, error) {
	raw, err := parseAny(data)
	if err != nil {
		return nil, err
	}
	b, err := json.Marshal(normalize(raw))
	if err != nil {
		return nil, fmt.Errorf("probe: %w", err)
	}
	var exp Export
	if err := json.Unmarshal(b, &exp); err != nil {
		return nil, fmt.Errorf("probe: %w", err)
	}
	if exp.Version == "" && len(exp.Snapshots) == 0 && len(exp.Ratings) == 0 {
		return nil, fmt.Errorf("probe: no addon data in this file (no version, snapshots or ratings)")
	}
	return &exp, nil
}

// parseAny decodes whichever of the two formats the bytes are in.
func parseAny(data []byte) (any, error) {
	trimmed := bytes.TrimLeft(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf")), " \t\r\n")
	if len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[') {
		var v any
		if err := json.Unmarshal(trimmed, &v); err != nil {
			return nil, fmt.Errorf("probe: %w", err)
		}
		return v, nil
	}
	return parseLuaChunk(string(trimmed), SavedVariable)
}

// normalize drops what the two formats disagree about: the addon's JSON writer
// prints an empty Lua table as [], which is a table in the saved variables and
// a list here, so empty containers and non-finite numbers are simply removed.
func normalize(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			if n := normalize(val); n != nil {
				out[k] = n
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	case []any:
		if len(t) == 0 {
			return nil
		}
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = normalize(val)
		}
		return out
	case float64:
		if math.IsNaN(t) || math.IsInf(t, 0) {
			return nil
		}
		return t
	}
	return v
}

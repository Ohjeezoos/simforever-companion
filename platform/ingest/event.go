package ingest

import (
	"fmt"
	"strings"
	"time"
)

type shape struct {
	kind   string
	suffix []int
	adv    bool
}

var advLayouts = map[int][]string{
	// The Forever client writes two fields nobody outside it has named, and
	// this is where they sit: everything before them reads as it always did,
	// and everything after is found by what it has to be. The cost is the
	// check - the client states what a spell costs, and index thirteen is
	// that number in 878 of 890 casts in the log this was read from.
	19: {"advGuid", "ownerGuid", "hp", "maxHp", "attackPower", "spellPower", "armor", "absorb",
		"unknown1", "unknown2", "powerType", "power", "maxPower", "powerCost", "x", "y", "map", "facing", "level"},
	17: {"advGuid", "ownerGuid", "hp", "maxHp", "attackPower", "spellPower", "armor", "absorb", "powerType", "power", "maxPower", "powerCost", "x", "y", "map", "facing", "level"},
	16: {"advGuid", "ownerGuid", "hp", "maxHp", "attackPower", "spellPower", "armor", "powerType", "power", "maxPower", "powerCost", "x", "y", "map", "facing", "level"},
	15: {"advGuid", "ownerGuid", "hp", "maxHp", "attackPower", "spellPower", "armor", "powerType", "power", "maxPower", "x", "y", "map", "facing", "level"},
}

var advFloat = map[string]bool{"x": true, "y": true, "facing": true}
var advString = map[string]bool{"advGuid": true, "ownerGuid": true}

// A unit with more than one kind of power states all of them at once, as lists
// down the four power fields in step: a rogue spending a finisher says energy
// and combo points, and the cost of each sits under the type it belongs to.
var advPower = map[string]bool{"powerType": true, "power": true, "maxPower": true, "powerCost": true}

func eventShape(name string) (prefix int, sh shape) {
	switch {
	case strings.HasPrefix(name, "SWING_"):
		prefix = 0
	case strings.HasPrefix(name, "ENVIRONMENTAL_"):
		prefix = 1
	case strings.HasPrefix(name, "SPELL_"), strings.HasPrefix(name, "RANGE_"), strings.HasPrefix(name, "DAMAGE_S"):
		prefix = 3
	}
	switch {
	case strings.HasSuffix(name, "_DAMAGE"), name == "SWING_DAMAGE_LANDED", name == "DAMAGE_SHIELD", name == "DAMAGE_SPLIT":
		sh = shape{"damage", []int{10, 11}, true}
	case strings.HasSuffix(name, "_MISSED"):
		sh = shape{"missed", []int{3, 4, 2, 1}, false}
	case strings.HasSuffix(name, "_HEAL"):
		sh = shape{"heal", []int{4, 5}, true}
	case strings.HasSuffix(name, "_ENERGIZE"):
		sh = shape{"energize", []int{4, 3, 2}, true}
	case strings.HasSuffix(name, "_DRAIN"), strings.HasSuffix(name, "_LEECH"):
		sh = shape{"drain", []int{3, 4}, true}
	case strings.HasSuffix(name, "_CAST_START"):
		sh = shape{"none", []int{0}, false}
	case strings.HasSuffix(name, "_CAST_SUCCESS"):
		sh = shape{"none", []int{0}, true}
	case strings.HasSuffix(name, "_CAST_FAILED"):
		sh = shape{"castFailed", []int{1}, false}
	case strings.HasSuffix(name, "_AURA_APPLIED_DOSE"), strings.HasSuffix(name, "_AURA_REMOVED_DOSE"):
		sh = shape{"aura", []int{2, 1}, false}
	case strings.HasSuffix(name, "_AURA_APPLIED"), strings.HasSuffix(name, "_AURA_REMOVED"), strings.HasSuffix(name, "_AURA_REFRESH"), strings.HasSuffix(name, "_AURA_BROKEN"):
		sh = shape{"aura", []int{1, 2}, false}
	case strings.HasSuffix(name, "_AURA_BROKEN_SPELL"):
		sh = shape{"extraSpellAura", []int{4}, false}
	case strings.HasSuffix(name, "_INTERRUPT"):
		sh = shape{"extraSpell", []int{3}, false}
	case strings.HasSuffix(name, "_DISPEL"), strings.HasSuffix(name, "_DISPEL_FAILED"), strings.HasSuffix(name, "_STOLEN"):
		sh = shape{"extraSpellAura", []int{4, 3}, false}
	case strings.HasSuffix(name, "_EXTRA_ATTACKS"):
		sh = shape{"amount", []int{1}, false}
	case strings.HasSuffix(name, "_INSTAKILL"):
		sh = shape{"raw", []int{1, 0}, false}
	case name == "UNIT_DIED", name == "UNIT_DESTROYED", name == "UNIT_DISSIPATES", name == "PARTY_KILL":
		sh = shape{"raw", []int{0, 1, 2}, false}
	default:
		sh = shape{"raw", nil, false}
	}
	return prefix, sh
}

// logVersion is what a file's COMBAT_LOG_VERSION line declared. Version 22,
// which Forever writes, states the hit before mitigation as the second field of
// every damage suffix. A count cannot tell you that on its own: with that field
// a swing's suffix is ten long, and ten is also the length of a suffix without
// it that ends in the off-hand flag. Reading Forever's swings the old way put
// the pre-mitigation number in overkill, -1 in the school, and shifted every
// flag after it, so 98 critical swings in one log were counted as glancing.
type logVersion int

// statesBaseAmount is whether a damage suffix's second field is the hit before
// mitigation.
func (v logVersion) statesBaseAmount(sufN int) bool {
	if v >= 22 {
		return sufN >= 10
	}
	return sufN == 11
}

func decodeEvent(t time.Time, f []string) (Event, error) {
	return decodeEventVersion(t, f, 0)
}

func decodeEventVersion(t time.Time, f []string, ver logVersion) (Event, error) {
	ev := Event{Time: t, Event: f[0]}
	switch f[0] {
	case "ENCOUNTER_START", "ENCOUNTER_END":
		if len(f) < 5 {
			return ev, fmt.Errorf("%s: want at least 4 fields, got %d", f[0], len(f)-1)
		}
		ev.Extra = map[string]any{
			"encounterId": atoi(f[1]),
			"name":        f[2],
			"difficulty":  atoi(f[3]),
			"groupSize":   atoi(f[4]),
		}
		if f[0] == "ENCOUNTER_START" {
			if len(f) > 5 {
				ev.Extra["instanceId"] = atoi(f[5])
			}
		} else if len(f) > 5 {
			ev.Extra["success"] = atob(f[5])
			if len(f) > 6 {
				ev.Extra["fightTimeMs"] = atoi(f[6])
			}
		}
		return ev, nil
	case "COMBATANT_INFO":
		if len(f) < 2 {
			return ev, fmt.Errorf("COMBATANT_INFO: missing GUID")
		}
		ev.SourceGUID = f[1]
		ev.Extra = map[string]any{"stats": parseCombatantInfo(f[2:])}
		return ev, nil
	case "COMBAT_LOG_VERSION", "ZONE_CHANGE", "MAP_CHANGE":
		ev.Extra = map[string]any{"fields": f[1:]}
		return ev, nil
	}
	if len(f) < 9 {
		return ev, fmt.Errorf("%s: want at least 8 base fields, got %d", f[0], len(f)-1)
	}
	ev.SourceGUID = nilStr(f[1])
	ev.SourceName = nilStr(f[2])
	ev.SourceFlags = atoi(f[3])
	ev.TargetGUID = nilStr(f[5])
	ev.TargetName = nilStr(f[6])
	ev.TargetFlags = atoi(f[7])
	prefix, sh := eventShape(f[0])
	rest := f[9:]
	if len(rest) < prefix {
		return ev, fmt.Errorf("%s: missing spell prefix", f[0])
	}
	switch prefix {
	case 3:
		ev.SpellID = int32(atoi(rest[0]))
		ev.SpellName = rest[1]
		ev.School = int32(atoi(rest[2]))
	case 1:
		ev.SpellName = rest[0]
		ev.set("environmentalType", rest[0])
	}
	rest = rest[prefix:]
	if sh.kind == "raw" && sh.suffix == nil {
		if len(rest) > 0 {
			ev.set("fields", rest)
		}
		return ev, nil
	}
	advN, sufN, ok := resolveCounts(len(rest), sh)
	if !ok {
		ev.set("fields", rest)
		return ev, fmt.Errorf("%s: unexpected field count %d", f[0], len(rest))
	}
	if advN > 0 {
		for i, key := range advLayouts[advN] {
			switch {
			case advString[key]:
				ev.set(key, nilStr(rest[i]))
			case advFloat[key]:
				ev.set(key, atof(rest[i]))
			case advPower[key]:
				list := powerList(rest[i])
				ev.set(key+"s", list)
				ev.set(key, list[0])
			default:
				ev.set(key, atoi(rest[i]))
			}
		}
		rest = rest[advN:]
	}
	suf := rest[:sufN]
	switch sh.kind {
	case "damage":
		if ver.statesBaseAmount(sufN) {
			ev.set("baseAmount", atoi(suf[1]))
			suf = append([]string{suf[0]}, suf[2:]...)
		}
		ev.Amount = atoi(suf[0])
		ev.Overkill = max(atoi(suf[1]), 0)
		if ev.SpellID == 0 && f[0] != "ENVIRONMENTAL_DAMAGE" {
			ev.School = int32(atoi(suf[2]))
		}
		// A school is a bitmask and an overkill is -1 or a number of points.
		// Neither can be anything else, so either one being impossible means
		// the fields were read at the wrong offsets, which is the one parser
		// mistake that produces numbers rather than an error.
		if school := atoi(suf[2]); school < 0 || school > 127 {
			return ev, fmt.Errorf("%s: read %d as the school, which is not one, so the %d suffix fields were taken at the wrong offsets",
				f[0], school, sufN)
		}
		ev.Resisted = atoi(suf[3])
		ev.Blocked = atoi(suf[4])
		ev.Absorbed = atoi(suf[5])
		ev.Critical = atob(suf[6])
		ev.Glancing = atob(suf[7])
		ev.Crushing = atob(suf[8])
		// The off-hand flag is the tenth, and a swing that states the hit
		// before mitigation does not carry one.
		if len(suf) > 9 && atob(suf[9]) {
			ev.set("isOffHand", true)
		}
	case "missed":
		ev.set("missType", suf[0])
		if sufN > 1 && atob(suf[1]) {
			ev.set("isOffHand", true)
		}
		if sufN > 2 {
			n := atoi(suf[2])
			ev.set("amountMissed", n)
			switch suf[0] {
			case "ABSORB":
				ev.Absorbed = n
			case "RESIST":
				ev.Resisted = n
			case "BLOCK":
				ev.Blocked = n
			}
		}
		if sufN > 3 {
			ev.Critical = atob(suf[3])
		}
	case "heal":
		if sufN == 5 {
			ev.set("baseAmount", atoi(suf[1]))
			suf = append([]string{suf[0]}, suf[2:]...)
		}
		ev.Amount = atoi(suf[0])
		ev.set("overhealing", atoi(suf[1]))
		ev.Absorbed = atoi(suf[2])
		ev.Critical = atob(suf[3])
	case "energize":
		ev.Amount = atoi(suf[0])
		if sufN > 1 {
			ev.set("overEnergize", atoi(suf[1]))
		}
		if sufN > 2 {
			ev.set("powerType", atoi(suf[2]))
		}
	case "drain":
		ev.Amount = atoi(suf[0])
		ev.set("powerType", atoi(suf[1]))
		ev.set("extraAmount", atoi(suf[2]))
	case "castFailed":
		ev.set("failedType", suf[0])
	case "aura":
		ev.set("auraType", suf[0])
		if sufN > 1 {
			ev.Amount = atoi(suf[1])
		}
	case "extraSpell", "extraSpellAura":
		ev.set("extraSpellId", atoi(suf[0]))
		ev.set("extraSpellName", suf[1])
		ev.set("extraSchool", atoi(suf[2]))
		if sufN > 3 {
			ev.set("auraType", suf[3])
		}
	case "amount":
		ev.Amount = atoi(suf[0])
	case "raw":
		if sufN > 0 {
			ev.set("fields", suf)
		}
	}
	return ev, nil
}

// resolveCounts picks the advanced-logging layout and suffix length whose sum
// matches the number of fields that follow the prefix.
func resolveCounts(n int, sh shape) (advN, sufN int, ok bool) {
	if sh.adv {
		// Longest-standing first. A count can be explained by more than one
		// layout - nineteen advanced fields and two suffix fields total the
		// same as seventeen and four - and the layouts that were here before
		// this client existed are the ones that have been checked against
		// every other client, so they get first refusal.
		for _, a := range []int{17, 16, 15, 19} {
			for _, s := range sh.suffix {
				if n == a+s {
					return a, s, true
				}
			}
		}
	}
	for _, s := range sh.suffix {
		if n == s {
			return 0, s, true
		}
	}
	return 0, 0, false
}

// powerList reads one power field. It always holds at least one entry, so a
// reader that wants the only power a unit has can take the first without
// asking whether there was one.
func powerList(s string) []int64 {
	if !strings.Contains(s, "|") {
		return []int64{atoi(s)}
	}
	parts := strings.Split(s, "|")
	out := make([]int64, len(parts))
	for i, p := range parts {
		out[i] = atoi(p)
	}
	return out
}

// AdvancedIsSource says whether the advanced block on this line describes the
// unit that caused the event. It does not always: this client writes the
// attacker's own numbers on a swing it made, and the victim's on one that
// landed on it. Reading the second as the first attributes a hit to whatever
// attack power the thing being hit happened to have.
func (e *Event) AdvancedIsSource() bool {
	guid, ok := e.Extra["advGuid"].(string)
	return ok && guid != "" && guid == e.SourceGUID
}

// PowerFor answers what this cast did to one kind of power - what it cost,
// what was left - or says the unit has no such power.
func (e *Event) PowerFor(field string, powerType int64) (int64, bool) {
	types, ok := e.Extra["powerTypes"].([]int64)
	if !ok {
		return 0, false
	}
	vals, ok := e.Extra[field+"s"].([]int64)
	if !ok {
		return 0, false
	}
	for i, t := range types {
		if t == powerType && i < len(vals) {
			return vals[i], true
		}
	}
	return 0, false
}

func (e *Event) set(k string, v any) {
	if e.Extra == nil {
		e.Extra = map[string]any{}
	}
	e.Extra[k] = v
}

func (e *Event) extraInt(k string) int64 {
	switch v := e.Extra[k].(type) {
	case int64:
		return v
	case float64:
		return int64(v)
	}
	return 0
}

func (e *Event) extraStr(k string) string {
	s, _ := e.Extra[k].(string)
	return s
}

func isDamage(name string) bool {
	if name == "SWING_DAMAGE_LANDED" {
		return false
	}
	return strings.HasSuffix(name, "_DAMAGE") || name == "DAMAGE_SHIELD" || name == "DAMAGE_SPLIT"
}

func isPeriodic(name string) bool {
	return strings.HasPrefix(name, "SPELL_PERIODIC_")
}

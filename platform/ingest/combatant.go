package ingest

import (
	"strconv"
	"strings"
)

// Scalar positions after the player GUID. The client emits the retail stat
// array; a Classic Era client that still has Spirit inserts it after intellect,
// which shows up as one extra scalar before the first bracket group.
var combatantStats = []string{
	"faction", "strength", "agility", "stamina", "intellect",
	"dodge", "parry", "block", "critMelee", "critRanged", "critSpell",
	"speed", "lifesteal", "hasteMelee", "hasteRanged", "hasteSpell",
	"avoidance", "mastery", "versatilityDamageDone", "versatilityHealingDone", "versatilityDamageTaken",
	"armor", "specId",
}

var combatantStatsSpirit = append(append([]string{}, combatantStats[:5]...), append([]string{"spirit"}, combatantStats[5:]...)...)

func parseCombatantInfo(f []string) map[string]any {
	stats := map[string]any{}
	scalars := 0
	for scalars < len(f) && !strings.HasPrefix(f[scalars], "[") && !strings.HasPrefix(f[scalars], "(") {
		scalars++
	}
	names := combatantStats
	if scalars == len(combatantStatsSpirit) {
		names = combatantStatsSpirit
	}
	for i := 0; i < scalars; i++ {
		key := strconv.Itoa(i)
		if i < len(names) {
			key = names[i]
		}
		stats[key] = atoi(f[i])
	}
	groups := f[scalars:]
	groupNames := []string{"talents", "pvpTalents", "items", "auras"}
	for i, g := range groups {
		key := "group" + strconv.Itoa(i)
		if i < len(groupNames) {
			key = groupNames[i]
		}
		switch key {
		case "items":
			stats[key] = parseItems(g)
		case "auras":
			stats[key] = parseAuras(g)
		default:
			if strings.HasPrefix(g, "[") || strings.HasPrefix(g, "(") {
				stats[key] = splitGroup(g)
			} else {
				stats[key] = atoi(g)
			}
		}
	}
	return stats
}

// items: [(itemId,itemLevel,(enchants),(gems),(bonusIds)),...]
func parseItems(g string) []map[string]any {
	var out []map[string]any
	for _, it := range splitGroup(g) {
		p := splitGroup(it)
		if len(p) == 0 {
			continue
		}
		m := map[string]any{"itemId": atoi(p[0])}
		if len(p) > 1 {
			m["itemLevel"] = atoi(p[1])
		}
		if len(p) > 2 {
			var ench []int64
			for _, e := range splitGroup(p[2]) {
				ench = append(ench, atoi(e))
			}
			if len(ench) > 0 {
				m["enchants"] = ench
			}
		}
		out = append(out, m)
	}
	return out
}

// auras: [casterGuid,spellId,casterGuid,spellId,...]
func parseAuras(g string) []map[string]any {
	items := splitGroup(g)
	var out []map[string]any
	for i := 0; i+1 < len(items); i += 2 {
		out = append(out, map[string]any{"sourceGuid": items[i], "spellId": atoi(items[i+1])})
	}
	return out
}

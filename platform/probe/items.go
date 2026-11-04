package probe

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/Ohjeezoos/simforever-companion/sim/core/proto"
)

// clientStatToStat maps the GetItemStats keys the client returns onto the
// wowsims stat names tools/db2 writes into UIItem.stats.
var clientStatToStat = map[string]string{
	"ITEM_MOD_STRENGTH_SHORT":             "StatStrength",
	"ITEM_MOD_AGILITY_SHORT":              "StatAgility",
	"ITEM_MOD_STAMINA_SHORT":              "StatStamina",
	"ITEM_MOD_INTELLECT_SHORT":            "StatIntellect",
	"ITEM_MOD_SPIRIT_SHORT":               "StatSpirit",
	"ITEM_MOD_HEALTH_SHORT":               "StatHealth",
	"ITEM_MOD_MANA_SHORT":                 "StatMana",
	"ITEM_MOD_ATTACK_POWER_SHORT":         "StatAttackPower",
	"ITEM_MOD_RANGED_ATTACK_POWER_SHORT":  "StatRangedAttackPower",
	"ITEM_MOD_SPELL_POWER_SHORT":          "StatSpellPower",
	"ITEM_MOD_SPELL_DAMAGE_DONE_SHORT":    "StatSpellPower",
	"ITEM_MOD_SPELL_HEALING_DONE_SHORT":   "StatHealingPower",
	"ITEM_MOD_MANA_REGENERATION_SHORT":    "StatMP5",
	"ITEM_MOD_POWER_REGEN0_SHORT":         "StatMP5",
	"ITEM_MOD_SPELL_PENETRATION_SHORT":    "StatSpellPenetration",
	"ITEM_MOD_BLOCK_VALUE_SHORT":          "StatBlockValue",
	"ITEM_MOD_DEFENSE_SKILL_RATING_SHORT": "StatDefense",
	"ITEM_MOD_DAMAGE_PER_SECOND_SHORT":    statWeaponDPS,
	"ITEM_MOD_RESILIENCE_RATING_SHORT":    "StatResilience",
	"RESISTANCE0_NAME":                    "StatArmor",
	"RESISTANCE2_NAME":                    "StatFireResistance",
	"RESISTANCE3_NAME":                    "StatNatureResistance",
	"RESISTANCE4_NAME":                    "StatFrostResistance",
	"RESISTANCE5_NAME":                    "StatShadowResistance",
	"RESISTANCE6_NAME":                    "StatArcaneResistance",
}

// clientStatToRating maps the client's rating keys onto the names
// tools/db2 gives ItemRecord.RatingStats. These are the ids that have to be
// right for Forever gear to convert at all.
var clientStatToRating = map[string]string{
	"ITEM_MOD_CRIT_RATING_SHORT":              "critRating",
	"ITEM_MOD_HIT_RATING_SHORT":               "hitRating",
	"ITEM_MOD_HASTE_RATING_SHORT":             "hasteRating",
	"ITEM_MOD_EXPERTISE_RATING_SHORT":         "expertiseRating",
	"ITEM_MOD_DODGE_RATING_SHORT":             "dodgeRating",
	"ITEM_MOD_PARRY_RATING_SHORT":             "parryRating",
	"ITEM_MOD_BLOCK_RATING_SHORT":             "blockRating",
	"ITEM_MOD_ARMOR_PENETRATION_RATING_SHORT": "armorPenetrationRating",
	"ITEM_MOD_CRIT_MELEE_RATING_SHORT":        "critMeleeRating",
	"ITEM_MOD_CRIT_RANGED_RATING_SHORT":       "critRangedRating",
	"ITEM_MOD_CRIT_SPELL_RATING_SHORT":        "critSpellRating",
	"ITEM_MOD_HIT_MELEE_RATING_SHORT":         "hitMeleeRating",
	"ITEM_MOD_HIT_RANGED_RATING_SHORT":        "hitRangedRating",
	"ITEM_MOD_HIT_SPELL_RATING_SHORT":         "hitSpellRating",
	"ITEM_MOD_HASTE_MELEE_RATING_SHORT":       "hasteMeleeRating",
	"ITEM_MOD_HASTE_RANGED_RATING_SHORT":      "hasteRangedRating",
	"ITEM_MOD_HASTE_SPELL_RATING_SHORT":       "hasteSpellRating",
	"ITEM_MOD_MASTERY_RATING_SHORT":           "masteryRating",
}

// statWeaponDPS is the one derived value worth checking: it proves the modern
// damage tables and DmgVariance, which no flat stat covers.
const statWeaponDPS = "weaponDPS"

// Reference is what the platform believes an item is: the converted UIItem
// stats plus, when the source carries them, the rating stats tools/db2 keeps
// outside the proto.
type Reference struct {
	ID         int32              `json:"id"`
	Name       string             `json:"name"`
	Ilvl       int32              `json:"ilvl"`
	Stats      map[string]float64 `json:"stats"`
	Ratings    map[string]float64 `json:"ratings"`
	HasRatings bool               `json:"hasRatings"`
}

// ReferenceFromJSON reads either a db2tool items entry
// ({"item":UIItem,"ratingStats":{...}}) or a bare UIItem, which is what the
// platform's items table stores.
func ReferenceFromJSON(data []byte) (Reference, error) {
	var wrapper struct {
		Item        json.RawMessage    `json:"item"`
		RatingStats map[string]float64 `json:"ratingStats"`
	}
	body := data
	var ref Reference
	if err := json.Unmarshal(data, &wrapper); err == nil && len(wrapper.Item) > 0 {
		body = wrapper.Item
		ref.Ratings, ref.HasRatings = wrapper.RatingStats, true
	}
	var ui struct {
		ID              int32     `json:"id"`
		Name            string    `json:"name"`
		Ilvl            int32     `json:"ilvl"`
		Stats           []float64 `json:"stats"`
		WeaponDamageMin float64   `json:"weaponDamageMin"`
		WeaponDamageMax float64   `json:"weaponDamageMax"`
		WeaponSpeed     float64   `json:"weaponSpeed"`
	}
	if err := json.Unmarshal(body, &ui); err != nil {
		return ref, fmt.Errorf("probe: item json: %w", err)
	}
	ref.ID, ref.Name, ref.Ilvl = ui.ID, ui.Name, ui.Ilvl
	ref.Stats = map[string]float64{}
	for i, v := range ui.Stats {
		if v == 0 {
			continue
		}
		name, ok := proto.Stat_name[int32(i)]
		if !ok {
			name = fmt.Sprintf("stat%d", i)
		}
		ref.Stats[name] += v
	}
	if ui.WeaponSpeed > 0 && ui.WeaponDamageMax > 0 {
		ref.Stats[statWeaponDPS] = (ui.WeaponDamageMin + ui.WeaponDamageMax) / 2 / ui.WeaponSpeed
	}
	if ref.Ratings == nil {
		ref.Ratings = map[string]float64{}
	}
	return ref, nil
}

type StatCompare struct {
	Stat     string  `json:"stat"`
	Client   float64 `json:"client"`
	Platform float64 `json:"platform"`
}

// ItemCheck is one item's comparison. Every field is per stat, because the
// point is to learn which stat the budget formula or the rating id gets wrong.
type ItemCheck struct {
	ItemID             int32         `json:"itemId"`
	ItemString         string        `json:"itemString"`
	Name               string        `json:"name"`
	Ilvl               int32         `json:"ilvl"`
	Matches            []StatCompare `json:"matches"`
	Mismatches         []StatCompare `json:"mismatches"`
	OnlyClient         []StatCompare `json:"onlyClient"`
	OnlyPlatform       []StatCompare `json:"onlyPlatform"`
	RatingsNotCompared []StatCompare `json:"ratingsNotCompared,omitempty"`
	Unmapped           []string      `json:"unmapped,omitempty"`
}

type StatSummary struct {
	Stat         string   `json:"stat"`
	Matched      int      `json:"matched"`
	Mismatched   int      `json:"mismatched"`
	OnlyClient   int      `json:"onlyClient"`
	OnlyPlatform int      `json:"onlyPlatform"`
	Examples     []string `json:"examples,omitempty"`
}

type ItemReport struct {
	Checked            int           `json:"checked"`
	NotInReference     []int32       `json:"notInReference"`
	Matched            int           `json:"matched"`
	Mismatched         int           `json:"mismatched"`
	OnlyClient         int           `json:"onlyClient"`
	OnlyPlatform       int           `json:"onlyPlatform"`
	RatingsNotCompared int           `json:"ratingsNotCompared"`
	Items              []ItemCheck   `json:"items"`
	Stats              []StatSummary `json:"stats"`
}

func tolerance(stat string, tol float64) float64 {
	if stat == statWeaponDPS {
		return math.Max(tol, 0.05)
	}
	return tol
}

// CheckItems compares the client's GetItemStats against the converted item for
// the same id. Nothing is written anywhere: the report is the product.
func CheckItems(exp *Export, ref map[int32]Reference, tol float64) ItemReport {
	if tol <= 0 {
		tol = 0.01
	}
	rep := ItemReport{NotInReference: []int32{}, Items: []ItemCheck{}, Stats: []StatSummary{}}
	byStat := map[string]*StatSummary{}
	stat := func(name string) *StatSummary {
		s := byStat[name]
		if s == nil {
			s = &StatSummary{Stat: name}
			byStat[name] = s
		}
		return s
	}
	itemStrings := make([]string, 0, len(exp.Items))
	for itemString := range exp.Items {
		itemStrings = append(itemStrings, itemString)
	}
	sort.Strings(itemStrings)
	for _, itemString := range itemStrings {
		item := exp.Items[itemString]
		id, ok := ItemID(itemString)
		if !ok {
			continue
		}
		r, known := ref[id]
		if !known {
			rep.NotInReference = append(rep.NotInReference, id)
			continue
		}
		rep.Checked++
		c := ItemCheck{ItemID: id, ItemString: itemString, Name: item.Name, Ilvl: item.Ilvl,
			Matches: []StatCompare{}, Mismatches: []StatCompare{}, OnlyClient: []StatCompare{}, OnlyPlatform: []StatCompare{}}
		client := map[string]float64{}
		for key, v := range item.Stats {
			switch {
			case v == 0 || skipClientKey(key):
			case clientStatToStat[key] != "":
				client[clientStatToStat[key]] += v
			case clientStatToRating[key] != "":
				client[clientStatToRating[key]] += v
			default:
				c.Unmapped = append(c.Unmapped, key)
			}
		}
		sort.Strings(c.Unmapped)
		for _, name := range sortedKeys(client) {
			cv := client[name]
			isRating := ratingName(name)
			pv, have := r.Stats[name]
			if isRating {
				pv, have = r.Ratings[name], r.HasRatings && r.Ratings[name] != 0
				if !r.HasRatings {
					c.RatingsNotCompared = append(c.RatingsNotCompared, StatCompare{Stat: name, Client: cv})
					rep.RatingsNotCompared++
					continue
				}
			}
			cmp := StatCompare{Stat: name, Client: cv, Platform: pv}
			switch {
			case !have:
				c.OnlyClient = append(c.OnlyClient, cmp)
				rep.OnlyClient++
				stat(name).OnlyClient++
				stat(name).example(fmt.Sprintf("%d %s: client %g, not converted", id, item.Name, cv))
			case math.Abs(cv-pv) <= tolerance(name, tol):
				c.Matches = append(c.Matches, cmp)
				rep.Matched++
				stat(name).Matched++
			default:
				c.Mismatches = append(c.Mismatches, cmp)
				rep.Mismatched++
				stat(name).Mismatched++
				stat(name).example(fmt.Sprintf("%d %s: client %g, platform %g", id, item.Name, cv, pv))
			}
		}
		for _, name := range sortedKeys(r.Stats) {
			if _, seen := client[name]; seen || r.Stats[name] == 0 {
				continue
			}
			c.OnlyPlatform = append(c.OnlyPlatform, StatCompare{Stat: name, Platform: r.Stats[name]})
			rep.OnlyPlatform++
			stat(name).OnlyPlatform++
			stat(name).example(fmt.Sprintf("%d %s: platform %g, client reports nothing", id, item.Name, r.Stats[name]))
		}
		rep.Items = append(rep.Items, c)
	}
	for _, name := range sortedSummaries(byStat) {
		rep.Stats = append(rep.Stats, *byStat[name])
	}
	return rep
}

func (s *StatSummary) example(line string) {
	if len(s.Examples) < 5 {
		s.Examples = append(s.Examples, line)
	}
}

func ratingName(stat string) bool { return strings.HasSuffix(stat, "Rating") }

// skipClientKey drops what the platform deliberately does not model.
func skipClientKey(key string) bool {
	return strings.HasPrefix(key, "EMPTY_SOCKET_") || key == "RESISTANCE1_NAME"
}

func sortedKeys(m map[string]float64) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedSummaries(m map[string]*StatSummary) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// LoadReferences reads a db2tool `items` dump, which is a JSON array of
// {"item": UIItem, "ratingStats": {...}} entries. This is the only source that
// carries the rating stats, because the proto has no home for them.
func LoadReferences(data []byte) (map[int32]Reference, error) {
	var raw []json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("probe: items file: %w", err)
	}
	out := make(map[int32]Reference, len(raw))
	for i, entry := range raw {
		ref, err := ReferenceFromJSON(entry)
		if err != nil {
			return nil, fmt.Errorf("probe: items file entry %d: %w", i, err)
		}
		if ref.ID != 0 {
			out[ref.ID] = ref
		}
	}
	return out, nil
}

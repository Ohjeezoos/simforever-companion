package probe

import (
	"encoding/json"
	"math"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/Ohjeezoos/simforever-companion/platform/ingest/fit"
)

func load(t *testing.T, path string) *Export {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	exp, err := Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	return exp
}

func TestParseSavedVariables(t *testing.T) {
	exp := load(t, "testdata/SimforeverProbeDB.lua")
	if exp.Version != "0.1.0" || exp.Build.String() != "1.15.9.69722" || !exp.Recording {
		t.Fatalf("header %+v %+v %v", exp.Version, exp.Build, exp.Recording)
	}
	if exp.Character.Name != "Emberlash" || exp.Character.Level != 60 || exp.Character.Class != "MAGE" {
		t.Errorf("character %+v", exp.Character)
	}
	if len(exp.Snapshots) != 2 || exp.Snapshots[0].ID != 1 || exp.Snapshots[1].Reason != "player_equipment_changed" {
		t.Fatalf("snapshots %+v", exp.Snapshots)
	}
	s := exp.Snapshots[0]
	if s.Stats.Intellect != 310 || s.Combat.CritChance != 6.1500000953674 || s.SpellPower["fire"] != 400 {
		t.Errorf("snapshot 1 %+v", s)
	}
	if got := s.Ratings["spellCrit"]; got.Rating != 14 || got.Percent != 1 {
		t.Errorf("snapshot ratings %+v", s.Ratings)
	}
	if len(s.Gear) != 1 || s.Gear[0].ID != 12640 || s.Gear[0].Slot != 1 {
		t.Errorf("gear %+v", s.Gear)
	}
	if len(exp.Snapshots[1].Gear) != 0 {
		t.Errorf("an empty lua table must stay empty, got %+v", exp.Snapshots[1].Gear)
	}
	if len(exp.Ratings) != 3 || exp.Ratings["spellCrit"]["77"].Percent != 5.5 || exp.Ratings["spellCrit"]["77"].Level != 60 {
		t.Errorf("ratings %+v", exp.Ratings)
	}
	if len(exp.Items) != 3 || exp.Items["item:12640:0:0:0:0:0:0:0:60:0:0:0"].Stats["ITEM_MOD_CRIT_RATING_SHORT"] != 28 {
		t.Errorf("items %+v", exp.Items)
	}
	if tgt := exp.Targets["Creature-0-4372-409-14-11502-000012A3"]; tgt.NpcID != 11502 || tgt.Level != 63 || tgt.Classification != "worldboss" {
		t.Errorf("targets %+v", exp.Targets)
	}
	if len(exp.Events) != 8 {
		t.Fatalf("events %d", len(exp.Events))
	}
	if e := exp.Events[2]; !e.Crit || e.Amount != 2030 {
		t.Errorf("crit event %+v", e)
	}
	if e := exp.Events[6]; e.Kind != "SWING_DAMAGE" || !e.Crit || e.Glancing {
		t.Errorf("the client reports crit as 1 and glancing as nil: %+v", e)
	}
	if e := exp.Events[7]; e.Kind != "fight_end" || e.N != 7 || e.At != 1758067340 {
		t.Errorf("fight_end %+v", e)
	}
}

// The saved variables and the JSON export are the same data in two formats, so
// they must parse to the same value.
func TestLuaAndJSONAgree(t *testing.T) {
	lua := load(t, "testdata/SimforeverProbeDB.lua")
	js := load(t, "testdata/export.json")
	lua.Recording = false // the JSON export does not carry it
	if !reflect.DeepEqual(lua, js) {
		t.Errorf("lua %+v\njson %+v", lua, js)
	}
}

func TestParseLuaEdges(t *testing.T) {
	src := `
-- a line comment
--[[ a
     block comment ]]
Other = { 1, 2 }
SimforeverProbeDB = {
	version = "0.2.0",
	["items"] = {
		["item:1:0"] = { name = "Quote \"me\"\nplease", stats = { ["ITEM_MOD_AGILITY_SHORT"] = 0x10 } },
	},
	["snapshots"] = { [1] = { id = 1, level = 1.5e1 }, [2] = { id = 2, level = -0.0 } },
	["ratings"] = {},
	["events"] = { { k = "SWING_DAMAGE", amount = 1 }; { k = "SWING_DAMAGE", amount = 2 } },
	["notes"] = { "kept" },
}
`
	exp, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if exp.Version != "0.2.0" {
		t.Errorf("version %q", exp.Version)
	}
	it := exp.Items["item:1:0"]
	if it.Name != "Quote \"me\"\nplease" || it.Stats["ITEM_MOD_AGILITY_SHORT"] != 16 {
		t.Errorf("item %+v", it)
	}
	if len(exp.Snapshots) != 2 || exp.Snapshots[0].Level != 15 || exp.Snapshots[1].ID != 2 {
		t.Errorf("snapshots %+v", exp.Snapshots)
	}
	if len(exp.Events) != 2 || exp.Events[1].Amount != 2 {
		t.Errorf("events %+v", exp.Events)
	}
	if len(exp.Ratings) != 0 {
		t.Errorf("an empty table is no readings, got %+v", exp.Ratings)
	}
	if len(exp.Notes) != 1 || exp.Notes[0] != "kept" {
		t.Errorf("notes %+v", exp.Notes)
	}
}

func TestParseLuaErrors(t *testing.T) {
	for _, tc := range []struct{ name, src string }{
		{"no global", `SomethingElse = { version = "1" }`},
		{"unterminated table", `SimforeverProbeDB = { version = "1",`},
		{"unterminated string", `SimforeverProbeDB = { version = "1 }`},
		{"no assignment", `SimforeverProbeDB { }`},
	} {
		if _, err := Parse([]byte(tc.src)); err == nil {
			t.Errorf("%s: expected an error", tc.name)
		}
	}
	if _, err := Parse([]byte(`{"nothing": 1}`)); err == nil {
		t.Error("a JSON file with no addon data must be rejected")
	}
}

func TestFitRatingsAgreeing(t *testing.T) {
	exp := load(t, "testdata/SimforeverProbeDB.lua")
	fits := FitRatings(exp, RatingOptions{})
	byKey := map[string]RatingFit{}
	for _, f := range fits {
		byKey[f.Key] = f
	}
	crit := byKey["spellCrit"]
	if crit.Conflict != "" {
		t.Fatalf("14/1, 28/2 and 77/5.5 are the same constant: %s", crit.Conflict)
	}
	if crit.PerPercent != 14 || crit.N != 3 || !crit.Verified || crit.Level != 60 {
		t.Errorf("spellCrit %+v", crit)
	}
	if crit.Min != 14 || crit.Max != 14 || crit.Spread != 0 {
		t.Errorf("spread %+v", crit)
	}
	if len(crit.Readings) != 3 || crit.Readings[0].Rating != 14 || crit.Readings[2].Rating != 77 {
		t.Errorf("readings %+v", crit.Readings)
	}
	if err := crit.Err(); err != nil {
		t.Errorf("err %v", err)
	}
}

func TestFitRatingsSingleReading(t *testing.T) {
	exp := load(t, "testdata/SimforeverProbeDB.lua")
	for _, f := range FitRatings(exp, RatingOptions{}) {
		if f.Key != "spellHit" {
			continue
		}
		if f.N != 1 || f.Conflict != "" {
			t.Fatalf("one reading is a measurement, not a conflict: %+v", f)
		}
		if math.Abs(f.PerPercent-12.6) > 1e-6 || f.Verified {
			t.Errorf("a single reading must not be verified: %+v", f)
		}
	}
}

func TestFitRatingsDisagreeing(t *testing.T) {
	exp := load(t, "testdata/SimforeverProbeDB.lua")
	var haste RatingFit
	for _, f := range FitRatings(exp, RatingOptions{}) {
		if f.Key == "meleeHaste" {
			haste = f
		}
	}
	if haste.Conflict == "" {
		t.Fatal("10 rating for 1% and 20 rating for 4% cannot both be true")
	}
	if err := haste.Err(); err == nil {
		t.Fatal("a conflict must be an error")
	}
	if haste.Verified {
		t.Error("a conflict is never verified")
	}
	run := Analyze(exp, nil, Options{})
	for _, row := range run.Rows(1, nil) {
		if row.Kind == KindRatingPerPercent && strings.HasPrefix(row.Notes, "meleeHaste") {
			t.Errorf("a disagreeing rating must not be stored: %+v", row)
		}
	}
	if len(run.Conflicts()) != 1 {
		t.Errorf("conflicts %+v", run.Conflicts())
	}
}

func TestFitRatingsTolerance(t *testing.T) {
	exp := &Export{Character: Character{Level: 60}, Ratings: map[string]map[string]Reading{
		"spellCrit": {"100": {Percent: 7.14, Level: 60}, "200": {Percent: 14.29, Level: 60}},
	}}
	f := FitRatings(exp, RatingOptions{})[0]
	if f.Conflict != "" {
		t.Errorf("client rounding must not read as a conflict: %s", f.Conflict)
	}
	if strict := FitRatings(exp, RatingOptions{Tolerance: 1e-9})[0]; strict.Conflict == "" {
		t.Error("a strict tolerance must reject them")
	}
}

func TestFitRatingsGroupsByLevel(t *testing.T) {
	exp := &Export{Ratings: map[string]map[string]Reading{
		"spellCrit": {"14": {Percent: 1, Level: 60}, "20": {Percent: 2, Level: 70}},
	}}
	fits := FitRatings(exp, RatingOptions{})
	if len(fits) != 2 || fits[0].Level != 60 || fits[1].Level != 70 {
		t.Fatalf("a conversion is per level: %+v", fits)
	}
	if fits[0].PerPercent != 14 || fits[1].PerPercent != 10 {
		t.Errorf("values %+v", fits)
	}
}

func TestCheckItems(t *testing.T) {
	exp := load(t, "testdata/SimforeverProbeDB.lua")
	raw, err := os.ReadFile("testdata/items_reference.json")
	if err != nil {
		t.Fatal(err)
	}
	ref, err := LoadReferences(raw)
	if err != nil {
		t.Fatal(err)
	}
	rep := CheckItems(exp, ref, 0)
	if rep.Checked != 2 || len(rep.NotInReference) != 1 || rep.NotInReference[0] != 99999 {
		t.Fatalf("checked %d, missing %v", rep.Checked, rep.NotInReference)
	}
	byItem := map[int32]ItemCheck{}
	for _, c := range rep.Items {
		byItem[c.ItemID] = c
	}
	helm := byItem[12640]
	got := map[string]StatCompare{}
	for _, m := range helm.Matches {
		got[m.Stat] = m
	}
	for _, want := range []string{"StatStrength", "StatArmor", "critRating"} {
		if _, ok := got[want]; !ok {
			t.Errorf("%s should match: %+v", want, helm)
		}
	}
	if len(helm.Mismatches) != 1 || helm.Mismatches[0].Stat != "hitRating" ||
		helm.Mismatches[0].Client != 14 || helm.Mismatches[0].Platform != 12 {
		t.Errorf("the wrong rating id must show up: %+v", helm.Mismatches)
	}
	if len(helm.OnlyPlatform) != 1 || helm.OnlyPlatform[0].Stat != "StatSpellPower" {
		t.Errorf("a stat the client does not report: %+v", helm.OnlyPlatform)
	}
	sword := byItem[19019]
	dps := false
	for _, m := range sword.Matches {
		if m.Stat == statWeaponDPS {
			dps = true
		}
	}
	if !dps {
		t.Errorf("weapon dps proves the damage tables: %+v", sword)
	}
	if len(sword.Unmapped) != 0 {
		t.Errorf("sockets are skipped, not unmapped: %v", sword.Unmapped)
	}
	if rep.Mismatched != 1 || rep.Matched < 5 {
		t.Errorf("totals %+v", rep)
	}
	for _, s := range rep.Stats {
		if s.Stat == "hitRating" && (s.Mismatched != 1 || len(s.Examples) != 1) {
			t.Errorf("per stat summary %+v", s)
		}
	}
}

func TestCheckItemsWithoutRatingReference(t *testing.T) {
	exp := load(t, "testdata/export.json")
	ref := map[int32]Reference{12640: {ID: 12640, Stats: map[string]float64{"StatStrength": 18, "StatArmor": 605}}}
	rep := CheckItems(exp, ref, 0)
	if rep.RatingsNotCompared != 2 || rep.Mismatched != 0 {
		t.Errorf("a reference with no ratings must say so, not report mismatches: %+v", rep)
	}
}

func TestFitCoefficients(t *testing.T) {
	exp := load(t, "testdata/SimforeverProbeDB.lua")
	fits, skipped := FitCoefficients(exp, fit.CoefOptions{Bootstrap: 200, Seed: 3}, "fixture")
	if len(fits) != 1 || fits[0].SpellID != 25306 {
		t.Fatalf("fits %+v skipped %+v", fits, skipped)
	}
	f := fits[0]
	if f.Samples != 3 {
		t.Fatalf("crits, resists and misses are excluded: %d samples", f.Samples)
	}
	if math.Abs(f.Coefficient.Value-59.0/60.0) > 1e-9 || f.Coefficient.Kind != "coefficient" {
		t.Errorf("coefficient %+v", f.Coefficient)
	}
	if math.Abs(f.Base.Value-616.6666666666667) > 1e-6 || f.Base.Kind != "base_damage" {
		t.Errorf("base %+v", f.Base)
	}
	if f.Coefficient.Method != "ols_bootstrap" || len(f.Coefficient.Sources) != 1 {
		t.Errorf("the log fitter's own method and sources must be kept: %+v", f.Coefficient)
	}
}

func TestBuildResultCarriesSnapshotSpellPower(t *testing.T) {
	exp := load(t, "testdata/SimforeverProbeDB.lua")
	res := BuildResult(exp, "fixture")
	if len(res.Fights) != 1 || len(res.Fights[0].Events) != 7 {
		t.Fatalf("fight %+v", res.Fights)
	}
	ev := res.Fights[0].Events[0]
	if ev.Extra["spellPower"] != 400.0 || ev.Extra["advGuid"] != exp.Character.GUID {
		t.Errorf("fire damage must carry the snapshot's fire power: %+v", ev.Extra)
	}
	if res.Fights[0].Events[3].Extra["spellPower"] != 700.0 {
		t.Errorf("the second snapshot's power must follow the gear change: %+v", res.Fights[0].Events[3].Extra)
	}
	if p, ok := schoolPower(4|32, map[string]float64{"fire": 400, "shadow": 500}); !ok || p != 500 {
		t.Errorf("a multi school hit uses the highest school: %v %v", p, ok)
	}
	if _, ok := schoolPower(4, map[string]float64{"frost": 100}); ok {
		t.Error("a school the snapshot never recorded is not a zero")
	}
}

func TestRows(t *testing.T) {
	exp := load(t, "testdata/SimforeverProbeDB.lua")
	run := Analyze(exp, nil, Options{Coef: fit.CoefOptions{Bootstrap: 100, Seed: 1}, Source: "fixture"})
	rows := run.Rows(7, nil)
	kinds := map[string]int{}
	for _, r := range rows {
		kinds[r.Kind]++
		if r.GameVersionID != 7 || r.Method == "" || len(r.Sources) == 0 {
			t.Fatalf("row %+v", r)
		}
	}
	if kinds[KindRatingPerPercent] != 2 || kinds["coefficient"] != 1 || kinds["base_damage"] != 1 {
		t.Errorf("kinds %v", kinds)
	}
	for _, r := range rows {
		if r.Kind != KindRatingPerPercent {
			continue
		}
		if r.Method != "simforever_probe/0.1.0" {
			t.Errorf("method must name the addon version: %q", r.Method)
		}
		var src map[string]any
		if err := json.Unmarshal(r.Sources, &src); err != nil {
			t.Fatal(err)
		}
		if src["ratingKey"] == nil || src["level"] == nil || src["readings"] == nil || src["character"] == nil {
			t.Errorf("sources %v", src)
		}
	}
}

func TestHiddenValuesTravelWithExport(t *testing.T) {
	lua := `SimforeverProbeDB = {
	["version"] = "0.2.0",
	["secrets"] = { ["UnitAttackPower"] = { ["#1"] = { ["combat"] = true }, ["#2"] = { ["combat"] = true } } },
	["blocked"] = { ["GetCombatRating"] = { ["at"] = 1789000000, ["by"] = "SimforeverProbe", ["fn"] = "GetCombatRating", ["named"] = true },
	["unnamed after GetItemStats"] = { ["at"] = 1789000001, ["by"] = "SimforeverProbe", ["named"] = false, ["lastCalls"] = { "UnitStat x5", "GetItemStats" } } },
	["snapshots"] = {},
	["events"] = {},
	["ratings"] = {},
	["items"] = {},
}`
	exp, err := Parse([]byte(lua))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	run := Analyze(exp, nil, Options{})
	got := run.Hidden()
	want := []string{"GetCombatRating (refused)", "UnitAttackPower (in combat)", "a refused action after UnitStat x5, GetItemStats"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("hidden = %v, want %v", got, want)
	}
}

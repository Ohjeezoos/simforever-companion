package ingest

import (
	"strings"
	"testing"
	"time"
)

const (
	pyra = "Player-4372-0000AAAA"
	grok = "Player-4372-0000BBBB"
	rag  = "Creature-0-4372-409-1-11502-0000000001"
)

func fixture(t *testing.T) *Result {
	t.Helper()
	res, err := ParseFileOptions("testdata/fixture.txt", ParseOptions{Year: 2026, Location: time.UTC})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestSplitFields(t *testing.T) {
	got := splitFields(`SPELL_DAMAGE,Player-1,"Name, With Comma",0x511,[(1,2),(3,4)],"x"`)
	want := []string{"SPELL_DAMAGE", "Player-1", "Name, With Comma", "0x511", "[(1,2),(3,4)]", "x"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q", got)
	}
}

func TestTimestamp(t *testing.T) {
	st, err := parseTimestamp("9/15 21:03:12.345")
	if err != nil || st.month != 9 || st.day != 15 || st.year != 0 || st.clock != 21*time.Hour+3*time.Minute+12345*time.Millisecond {
		t.Fatalf("got %+v %v", st, err)
	}
	st, err = parseTimestamp("1/2/2027 00:00:01.000-5")
	if err != nil || st.year != 2027 || st.month != 1 || st.clock != time.Second {
		t.Fatalf("got %+v %v", st, err)
	}
}

func TestYearRollover(t *testing.T) {
	log := "12/31 23:59:59.000  SWING_DAMAGE,Player-1-A,\"A\",0x511,0x0,Creature-0-1-1-1-1-1,\"B\",0x10a48,0x0,10,0,1,0,0,0,nil,nil,nil,nil\n" +
		"1/1 00:00:01.000  SWING_DAMAGE,Player-1-A,\"A\",0x511,0x0,Creature-0-1-1-1-1-1,\"B\",0x10a48,0x0,10,0,1,0,0,0,nil,nil,nil,nil\n"
	res, err := Parse(strings.NewReader(log), ParseOptions{Year: 2026, Location: time.UTC})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Fights) != 1 || len(res.Fights[0].Events) != 2 {
		t.Fatalf("fights %+v", res.Fights)
	}
	ev := res.Fights[0].Events
	if ev[0].Time.Year() != 2026 || ev[1].Time.Year() != 2027 || ev[1].Ms != 2000 {
		t.Fatalf("times %v %v ms %d", ev[0].Time, ev[1].Time, ev[1].Ms)
	}
}

// The Forever client writes two advanced fields nobody outside it has named.
// Where they sit was found by what the cost has to be: the client states what a
// spell costs, and only one position holds that number.
func TestForeverAdvancedLayout(t *testing.T) {
	const line = `9/20/2026 15:10:07.684  SPELL_DAMAGE,Player-1,"Caster",0x518,0x0,` +
		`Creature-0-1-1-1-299-0,"Kobold",0xa48,0x0,686,"Shadow Bolt",0x20,` +
		`Player-1,0000000000000000,282,804,182,31,295,0,0,0,0,1146,1378,110,1806.89,-4375.85,1414,1.9306,17,` +
		`142,0,32,0,0,0,nil,nil,nil,nil`
	res, err := Parse(strings.NewReader(line), ParseOptions{Year: 2026})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Warnings) != 0 {
		t.Fatalf("nineteen advanced fields were not understood: %v", res.Warnings)
	}
	if len(res.Fights) == 0 || len(res.Fights[0].Events) == 0 {
		t.Fatal("the line parsed into no event")
	}
	e := res.Fights[0].Events[0]
	for _, c := range []struct {
		key  string
		want int64
	}{
		{"attackPower", 182},
		{"spellPower", 31},
		{"armor", 295},
		{"power", 1146},
		{"maxPower", 1378},
		{"powerCost", 110}, // what the client says Shadow Bolt costs
		{"level", 17},
	} {
		if got := e.Extra[c.key]; got != c.want {
			t.Errorf("%s = %v, want %v", c.key, got, c.want)
		}
	}
	// And the damage after them, so a layout that swallowed a field would
	// show up as the hit landing for the wrong number.
	if e.Amount != 142 {
		t.Errorf("damage = %d, want 142", e.Amount)
	}
	for _, c := range []struct {
		key  string
		want int64
	}{} {
		if got := e.Extra[c.key]; got != c.want {
			t.Errorf("%s = %v, want %v", c.key, got, c.want)
		}
	}
}

func TestParseFixture(t *testing.T) {
	res := fixture(t)
	if res.Lines != 47 {
		t.Errorf("lines = %d", res.Lines)
	}
	if len(res.Warnings) != 2 || !strings.Contains(res.Warnings[0], "line 30") || !strings.Contains(res.Warnings[1], "line 31") {
		t.Errorf("warnings = %v", res.Warnings)
	}
	if len(res.Fights) != 4 {
		t.Fatalf("fights = %d", len(res.Fights))
	}
	f := res.Fights[0]
	byEvent := map[string][]Event{}
	for _, ev := range f.Events {
		byEvent[ev.Event] = append(byEvent[ev.Event], ev)
	}
	dmg := byEvent["SPELL_DAMAGE"]
	if len(dmg) != 5 {
		t.Fatalf("spell damage events = %d", len(dmg))
	}
	first := dmg[0]
	if first.SourceGUID != pyra || first.SourceName != "Pyra" || first.TargetGUID != rag || first.TargetName != "Ragnaros" {
		t.Errorf("base fields %+v", first)
	}
	if first.SourceFlags != 0x511 || first.TargetFlags != 0x10a48 {
		t.Errorf("flags %x %x", first.SourceFlags, first.TargetFlags)
	}
	if first.SpellID != 25304 || first.SpellName != "Fireball" || first.School != 4 || first.Amount != 850 || first.Critical {
		t.Errorf("spell/suffix %+v", first)
	}
	if first.Extra["advGuid"] != pyra || first.Extra["spellPower"] != int64(250) || first.Extra["hp"] != int64(3200) || first.Extra["level"] != int64(60) || first.Extra["x"] != -123.45 || first.Extra["map"] != int64(409) {
		t.Errorf("advanced %+v", first.Extra)
	}
	crit := dmg[3]
	if !crit.Critical || crit.Amount != 1700 {
		t.Errorf("crit %+v", crit)
	}
	if partial := dmg[4]; partial.Resisted != 213 {
		t.Errorf("partial resist %+v", partial)
	}
	swing := byEvent["SWING_DAMAGE"][0]
	if swing.SpellID != 0 || swing.Amount != 120 || !swing.Glancing || swing.School != 1 || swing.Extra["attackPower"] != int64(900) {
		t.Errorf("swing %+v", swing)
	}
	if miss := byEvent["SWING_MISSED"][0]; miss.Extra["missType"] != "DODGE" {
		t.Errorf("swing miss %+v", miss)
	}
	if miss := byEvent["SPELL_MISSED"][0]; miss.Extra["missType"] != "RESIST" || miss.SpellID != 25304 {
		t.Errorf("spell miss %+v", miss)
	}
	if heal := byEvent["SPELL_HEAL"][0]; heal.Amount != 400 || heal.Extra["overhealing"] != int64(50) || heal.Extra["advGuid"] != grok {
		t.Errorf("heal %+v", heal)
	}
	if en := byEvent["SPELL_ENERGIZE"][0]; en.Amount != 10 || en.Extra["powerType"] != int64(1) {
		t.Errorf("energize %+v", en)
	}
	if cf := byEvent["SPELL_CAST_FAILED"][0]; cf.Extra["failedType"] != "Not enough mana" {
		t.Errorf("cast failed %+v", cf)
	}
	if cs := byEvent["SPELL_CAST_SUCCESS"][0]; cs.Extra["powerCost"] != int64(290) || cs.Extra["spellPower"] != int64(250) {
		t.Errorf("cast success advanced %+v", cs)
	}
	if dose := byEvent["SPELL_AURA_APPLIED_DOSE"][0]; dose.Amount != 2 || dose.Extra["auraType"] != "BUFF" {
		t.Errorf("dose %+v", dose)
	}
	if ap := byEvent["SPELL_AURA_APPLIED"][0]; ap.Extra["auraType"] != "DEBUFF" || ap.Amount != 0 {
		t.Errorf("aura applied %+v", ap)
	}
	if len(byEvent["SPELL_AURA_REFRESH"]) != 1 || len(byEvent["SPELL_AURA_REMOVED"]) != 2 || len(byEvent["SPELL_PERIODIC_DAMAGE"]) != 1 || len(byEvent["SPELL_CAST_START"]) != 1 {
		t.Errorf("event families missing: %v", byEvent)
	}
	if died := byEvent["UNIT_DIED"][0]; died.TargetGUID != rag || died.SourceGUID != "" {
		t.Errorf("unit died %+v", died)
	}
	if es := byEvent["ENCOUNTER_START"][0]; es.Extra["encounterId"] != int64(663) || es.Extra["groupSize"] != int64(40) || es.Extra["instanceId"] != int64(409) {
		t.Errorf("encounter start %+v", es.Extra)
	}
	if ee := byEvent["ENCOUNTER_END"][0]; ee.Extra["success"] != true || ee.Extra["fightTimeMs"] != int64(20000) {
		t.Errorf("encounter end %+v", ee.Extra)
	}
}

func TestPlayersAndCombatantInfo(t *testing.T) {
	f := fixture(t).Fights[0]
	if len(f.Players) != 2 {
		t.Fatalf("players %+v", f.Players)
	}
	p, g := f.Players[0], f.Players[1]
	// A player's level is never stated on any line, so it stays unset even
	// though the advanced block ends in a number.
	if p.GUID != pyra || p.Name != "Pyra" || p.Class != "Mage" || p.Level != 0 {
		t.Errorf("pyra %+v", p)
	}
	if g.GUID != grok || g.Class != "Warrior" {
		t.Errorf("grok %+v", g)
	}
	if p.Stats["strength"] != int64(45) || p.Stats["intellect"] != int64(250) || p.Stats["armor"] != int64(120) || p.Stats["specId"] != int64(0) {
		t.Errorf("pyra stats %+v", p.Stats)
	}
	if _, has := p.Stats["spirit"]; has {
		t.Errorf("retail layout must not carry spirit")
	}
	items, _ := p.Stats["items"].([]map[string]any)
	if len(items) != 2 || items[1]["itemId"] != int64(18820) || items[1]["enchants"].([]int64)[0] != 2543 {
		t.Errorf("items %+v", items)
	}
	auras, _ := p.Stats["auras"].([]map[string]any)
	if len(auras) != 2 || auras[1]["spellId"] != int64(25289) {
		t.Errorf("auras %+v", auras)
	}
	if g.Stats["spirit"] != int64(60) || g.Stats["dodge"] != int64(5) || g.Stats["critMelee"] != int64(12) || g.Stats["armor"] != int64(3500) {
		t.Errorf("spirit layout %+v", g.Stats)
	}
	if f2 := fixture(t).Fights[1]; f2.Players[0].Stats == nil {
		t.Errorf("stats from the last COMBATANT_INFO before the fight should carry over")
	}
}

func TestUnknownStatPositions(t *testing.T) {
	st := parseCombatantInfo(splitFields("0,1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20,21,22,23,24,25,26,[],[],[],[]")[0:])
	if st["26"] != int64(26) || st["faction"] != int64(0) || st["specId"] != int64(22) {
		t.Errorf("stats %+v", st)
	}
}

func TestAdvancedLayoutDetection(t *testing.T) {
	base := `SPELL_DAMAGE,Player-1-A,"A",0x511,0x0,Creature-0-1-1-1-1-1,"B",0x10a48,0x0,116,"Frostbolt",0x10,`
	suffix := `,500,0,16,0,0,0,nil,nil,nil,nil`
	cases := map[string]int64{
		"Player-1-A,0000000000000000,1,2,3,777,5,6,0,7,8,9,1.0,2.0,3,4.0,60": 777,
		"Player-1-A,0000000000000000,1,2,3,666,5,0,7,8,9,1.0,2.0,3,4.0,60":   666,
		"Player-1-A,0000000000000000,1,2,3,555,5,0,7,8,1.0,2.0,3,4.0,60":     555,
	}
	for adv, sp := range cases {
		ev, err := decodeEvent(time.Time{}, splitFields(base+adv+suffix))
		if err != nil || ev.Extra["spellPower"] != sp || ev.Extra["level"] != int64(60) || ev.Amount != 500 {
			t.Errorf("%d fields: %v %+v", len(strings.Split(adv, ",")), err, ev.Extra)
		}
	}
	ev, err := decodeEvent(time.Time{}, splitFields(base[:len(base)-1]+suffix))
	if err != nil || ev.Extra != nil || ev.Amount != 500 {
		t.Errorf("plain: %v %+v", err, ev)
	}
	if _, err := decodeEvent(time.Time{}, splitFields(base+"1,2,3")); err == nil {
		t.Errorf("want field count error")
	}
}

func TestClassHeuristicConflict(t *testing.T) {
	log := "9/15 21:00:00.000  SPELL_CAST_SUCCESS,Player-1-A,\"A\",0x511,0x0,0000000000000000,nil,0x80000000,0x80000000,116,\"Frostbolt\",0x10\n" +
		"9/15 21:00:01.000  SPELL_CAST_SUCCESS,Player-1-A,\"A\",0x511,0x0,0000000000000000,nil,0x80000000,0x80000000,686,\"Shadow Bolt\",0x20\n" +
		"9/15 21:00:02.000  SPELL_DAMAGE,Player-1-A,\"A\",0x511,0x0,Creature-0-1-1-1-1-1,\"B\",0x10a48,0x0,116,\"Frostbolt\",0x10,500,0,16,0,0,0,nil,nil,nil,nil\n"
	res, err := Parse(strings.NewReader(log), ParseOptions{Year: 2026})
	if err != nil {
		t.Fatal(err)
	}
	if res.Fights[0].Players[0].Class != "" {
		t.Errorf("conflicting signatures must leave class empty, got %q", res.Fights[0].Players[0].Class)
	}
}

// A rogue spending a finisher states energy and combo points at once, as lists
// down the four power fields in step. Taken verbatim from a Forever log.
func TestPowerFieldsAreListsWhenAUnitHasSeveralPowers(t *testing.T) {
	line := `9/20/2026 16:22:47.1182  SPELL_CAST_SUCCESS,Player-4613-00A26D87,"Hails",0x518,0x80000000,` +
		`Creature-0-6782-1-11803-3197-0000AFEBE8,"Burning Blade Fanatic",0xa28,0x80000000,6760,"Eviscerate",0x1,` +
		`Player-4613-00A26D87,0000000000000000,202,276,137,3,304,0,0,0,3|4,43|2,100|5,35|2,1438.59,-4787.19,1411,2.4125,3`
	_, payload, ok := splitLine(line)
	if !ok {
		t.Fatal("the line does not split into a timestamp and a payload")
	}
	got, err := decodeEvent(time.Time{}, splitFields(payload))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	e := &got
	if e.SpellID != 6760 {
		t.Fatalf("decoded the wrong spell: %+v", e)
	}

	types, ok := e.Extra["powerTypes"].([]int64)
	if !ok || len(types) != 2 || types[0] != 3 || types[1] != 4 {
		t.Fatalf("power types %v", e.Extra["powerTypes"])
	}
	costs, ok := e.Extra["powerCosts"].([]int64)
	if !ok || len(costs) != 2 || costs[0] != 35 || costs[1] != 2 {
		t.Fatalf("power costs %v", e.Extra["powerCosts"])
	}
	if got, ok := e.PowerFor("powerCost", 3); !ok || got != 35 {
		t.Fatalf("energy spent: got %d ok=%v, want 35", got, ok)
	}
	if got, ok := e.PowerFor("power", 4); !ok || got != 2 {
		t.Fatalf("combo points left: got %d ok=%v, want 2", got, ok)
	}
	if _, ok := e.PowerFor("powerCost", 1); ok {
		t.Fatal("a rogue was charged rage")
	}
	// The plain name still answers for a reader that only wants one.
	if e.Extra["powerCost"] != int64(35) {
		t.Fatalf("scalar power cost %v", e.Extra["powerCost"])
	}
}

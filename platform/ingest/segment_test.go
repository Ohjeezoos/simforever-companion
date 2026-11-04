package ingest

import (
	"strings"
	"testing"
	"time"
)

func at(h, m, s int, ms int) time.Time {
	return time.Date(2026, 9, 15, h, m, s, ms*1e6, time.UTC)
}

func TestEncounterFight(t *testing.T) {
	f := fixture(t).Fights[0]
	if f.EncounterID != 663 || f.EncounterName != "Ragnaros" || f.Success == nil || !*f.Success {
		t.Errorf("encounter %+v", f)
	}
	if !f.StartedAt.Equal(at(21, 0, 0, 0)) || !f.EndedAt.Equal(at(21, 0, 20, 0)) || f.DurationMs != 20000 {
		t.Errorf("times %v %v %d", f.StartedAt, f.EndedAt, f.DurationMs)
	}
	if f.Events[0].Event != "ENCOUNTER_START" || f.Events[len(f.Events)-1].Event != "ENCOUNTER_END" {
		t.Errorf("edges %s %s", f.Events[0].Event, f.Events[len(f.Events)-1].Event)
	}
	if f.Events[1].Ms != 1000 {
		t.Errorf("ms offset %d", f.Events[1].Ms)
	}
}

func TestGapFightEndsOnMainTargetDeath(t *testing.T) {
	f := fixture(t).Fights[1]
	if f.EncounterID != 0 || f.Success != nil {
		t.Errorf("gap fight %+v", f)
	}
	if !f.StartedAt.Equal(at(21, 0, 58, 500)) {
		t.Errorf("start should be the opener cast start, got %v", f.StartedAt)
	}
	if !f.EndedAt.Equal(at(21, 1, 10, 0)) || f.DurationMs != 11500 {
		t.Errorf("end %v %d", f.EndedAt, f.DurationMs)
	}
	for _, ev := range f.Events {
		if ev.SpellName == "Arcane Intellect" {
			t.Errorf("buff outside the lookback window must not join the fight")
		}
		if ev.Ms < 0 {
			t.Errorf("negative offset %+v", ev)
		}
	}
	if f.Events[0].Event != "SPELL_CAST_START" || f.Events[len(f.Events)-1].Event != "UNIT_DIED" {
		t.Errorf("edges %s %s", f.Events[0].Event, f.Events[len(f.Events)-1].Event)
	}
}

func TestGapFightEndsOnGap(t *testing.T) {
	res := fixture(t)
	f3, f4 := res.Fights[2], res.Fights[3]
	if !f3.StartedAt.Equal(at(21, 1, 45, 0)) || !f3.EndedAt.Equal(at(21, 1, 47, 0)) || len(f3.Events) != 2 {
		t.Errorf("third fight %v %v %d", f3.StartedAt, f3.EndedAt, len(f3.Events))
	}
	if !f4.StartedAt.Equal(at(21, 2, 30, 0)) || f4.DurationMs != 0 || len(f4.Events) != 1 {
		t.Errorf("fourth fight %v %d %d", f4.StartedAt, f4.DurationMs, len(f4.Events))
	}
}

func TestGapOption(t *testing.T) {
	res, err := ParseFileOptions("testdata/fixture.txt", ParseOptions{Year: 2026, GapSeconds: 60, Location: time.UTC})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Fights) != 3 {
		t.Errorf("with a 60s gap the last two fights merge, got %d", len(res.Fights))
	}
}

func TestUnclosedEncounter(t *testing.T) {
	log := "9/15 21:00:00.000  ENCOUNTER_START,663,\"Ragnaros\",9,40,409\n" +
		"9/15 21:00:01.000  SWING_DAMAGE,Player-1-A,\"A\",0x511,0x0,Creature-0-1-1-1-1-1,\"B\",0x10a48,0x0,10,0,1,0,0,0,nil,nil,nil,nil\n" +
		"9/15 21:00:02.000  ENCOUNTER_END,1,\"Other\",9,40,0\n" +
		"9/15 21:00:03.000  ENCOUNTER_END,1,\"Other\",9,40,0\n"
	res, err := Parse(strings.NewReader(log), ParseOptions{Year: 2026})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Fights) != 1 || res.Fights[0].Success == nil || *res.Fights[0].Success {
		t.Errorf("fights %+v", res.Fights)
	}
	if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "ENCOUNTER_END without") {
		t.Errorf("warnings %v", res.Warnings)
	}
}

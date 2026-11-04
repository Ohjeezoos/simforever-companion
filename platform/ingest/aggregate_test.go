package ingest

import (
	"math"
	"testing"
)

func TestDamageBySpell(t *testing.T) {
	f := fixture(t).Fights[0]
	d := DamageBySpell(&f, pyra)
	if len(d) != 1 {
		t.Fatalf("spells %+v", d)
	}
	fb := d[0]
	if fb.SpellID != 25304 || fb.Hits != 4 || fb.Ticks != 1 || fb.Crits != 1 || fb.Total != 4069 || fb.Min != 19 || fb.Max != 1700 || fb.Casts != 4 {
		t.Errorf("fireball %+v", fb)
	}
	if math.Abs(fb.Mean-4069.0/5) > 1e-9 || fb.Misses["RESIST"] != 1 {
		t.Errorf("fireball mean/misses %+v", fb)
	}
	g := DamageBySpell(&f, grok)
	if len(g) != 2 || g[0].SpellName != "Bloodthirst" || g[0].Total != 400 || g[0].Casts != 1 {
		t.Errorf("grok %+v", g)
	}
	melee := g[1]
	if melee.SpellID != 0 || melee.SpellName != "Melee" || melee.Hits != 2 || melee.Total != 370 || melee.Crits != 1 || melee.Misses["DODGE"] != 1 {
		t.Errorf("melee must ignore SWING_DAMAGE_LANDED: %+v", melee)
	}
}

func TestDPS(t *testing.T) {
	f := fixture(t).Fights[0]
	if TotalDamage(&f, pyra) != 4069 || math.Abs(DPS(&f, pyra)-203.45) > 1e-9 {
		t.Errorf("dps %v", DPS(&f, pyra))
	}
	if DPS(&f, grok) != 38.5 {
		t.Errorf("grok dps %v", DPS(&f, grok))
	}
}

func TestUptime(t *testing.T) {
	f := fixture(t).Fights[0]
	u := Uptime(&f, grok, 12880)
	if u.UptimeMs != 5000 || u.Fraction != 0.25 || u.Applies != 1 || u.Refreshes != 2 || u.SpellName != "Enrage" {
		t.Errorf("enrage %+v", u)
	}
	debuff := Uptime(&f, rag, 25304)
	if debuff.UptimeMs != 8000 {
		t.Errorf("fireball dot %+v", debuff)
	}
	f2 := fixture(t).Fights[1]
	if open := Uptime(&f2, pyra, 10157); open.UptimeMs != 0 {
		t.Errorf("aura applied before the fight with no event inside it: %+v", open)
	}
}

func TestCastSequence(t *testing.T) {
	f := fixture(t).Fights[0]
	seq := CastSequence(&f, pyra)
	if len(seq) != 5 || seq[0].Event != "SPELL_CAST_START" || seq[0].Ms != 1000 || seq[1].Event != "SPELL_CAST_SUCCESS" || seq[1].Ms != 4000 || seq[1].TargetName != "Ragnaros" {
		t.Errorf("sequence %+v", seq)
	}
	for i := 1; i < len(seq); i++ {
		if seq[i].Ms < seq[i-1].Ms {
			t.Errorf("out of order at %d", i)
		}
	}
}

func TestSummarize(t *testing.T) {
	f := fixture(t).Fights[0]
	s := Summarize(&f)
	if s.EncounterName != "Ragnaros" || len(s.Players) != 2 || s.Players[0].Casts != 4 || s.Players[0].Damage != 4069 || s.Events != 30 {
		t.Errorf("summary %+v", s)
	}
}

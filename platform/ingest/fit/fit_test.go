package fit

import (
	"errors"
	"math"
	"math/rand"
	"testing"
	"time"

	"github.com/Ohjeezoos/simforever-companion/platform/ingest"
)

func synthetic(n int, base, coef float64, seed int64) []Sample {
	rng := rand.New(rand.NewSource(seed))
	sps := []float64{100, 250, 400, 550}
	out := make([]Sample, 0, n)
	for i := 0; i < n; i++ {
		sp := sps[i%len(sps)]
		roll := base*0.9 + rng.Float64()*base*0.2
		out = append(out, Sample{SpellPower: sp, Damage: math.Floor(roll + coef*sp), Source: "synthetic"})
	}
	return out
}

func TestCoefficientRecoversKnownValue(t *testing.T) {
	samples := synthetic(400, 600, 0.857, 7)
	coef, base, err := Coefficient(25304, samples, CoefOptions{Bootstrap: 500, Seed: 1})
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(coef.Value-0.857) > 0.03 {
		t.Errorf("coef %v", coef.Value)
	}
	if math.Abs(base.Value-600) > 15 {
		t.Errorf("base %v", base.Value)
	}
	if !(coef.CILow < 0.857 && 0.857 < coef.CIHigh) || coef.CIHigh-coef.CILow > 0.1 {
		t.Errorf("ci %v %v", coef.CILow, coef.CIHigh)
	}
	if coef.Kind != "coefficient" || base.Kind != "base_damage" || coef.N != 400 || coef.Method != "ols_bootstrap" || coef.SpellID != 25304 || len(coef.Sources) != 1 {
		t.Errorf("measurement %+v", coef)
	}
	again, _, _ := Coefficient(25304, samples, CoefOptions{Bootstrap: 500, Seed: 1})
	if again.CILow != coef.CILow {
		t.Errorf("bootstrap must be deterministic for a seed")
	}
}

func TestCoefficientNeedsSpread(t *testing.T) {
	var s []Sample
	for i := 0; i < 10; i++ {
		s = append(s, Sample{SpellPower: 300, Damage: 900 + float64(i)})
	}
	if _, _, err := Coefficient(1, s, CoefOptions{}); !errors.Is(err, ErrNoSpread) {
		t.Errorf("err = %v", err)
	}
	if _, _, err := Coefficient(1, s[:2], CoefOptions{}); !errors.Is(err, ErrTooFew) {
		t.Errorf("err = %v", err)
	}
}

func TestCollectSamplesFromFixture(t *testing.T) {
	res, err := ingest.ParseFileOptions("../testdata/fixture.txt", ingest.ParseOptions{Year: 2026, Location: time.UTC})
	if err != nil {
		t.Fatal(err)
	}
	s := CollectSamples(res, 25304, "fixture")
	if len(s) != 4 {
		t.Fatalf("crit, resist, periodic and miss must be excluded: %+v", s)
	}
	coef, base, err := Coefficient(25304, s, CoefOptions{Bootstrap: 200, Seed: 3})
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(coef.Value-1) > 1e-9 || math.Abs(base.Value-605) > 1e-9 || coef.Sources[0] != "fixture" {
		t.Errorf("coef %+v base %+v", coef, base)
	}
}

func TestCollectSamplesFallsBackToStats(t *testing.T) {
	res := &ingest.Result{Fights: []ingest.Fight{{
		Players: []ingest.Player{{GUID: "Player-1", Stats: map[string]any{"spellPower": int64(200)}}},
		Events: []ingest.Event{
			{Event: "SPELL_DAMAGE", SourceGUID: "Player-1", SpellID: 5, Amount: 700},
			{Event: "SPELL_DAMAGE", SourceGUID: "Player-2", SpellID: 5, Amount: 700},
		},
	}}}
	s := CollectSamples(res, 5, "x")
	if len(s) != 1 || s[0].SpellPower != 200 {
		t.Errorf("samples %+v", s)
	}
}

func TestProcRate(t *testing.T) {
	ppm, chance, err := ProcRate(12880, ProcCounts{Triggers: 200, Procs: 40, DurationMs: 600000, Sources: []string{"a"}}, 0.95)
	if err != nil {
		t.Fatal(err)
	}
	if chance.Value != 0.2 || chance.Kind != "proc_chance" || chance.Method != "wilson" || chance.N != 200 {
		t.Errorf("chance %+v", chance)
	}
	if math.Abs(chance.CILow-0.1501) > 1e-3 || math.Abs(chance.CIHigh-0.2612) > 1e-3 {
		t.Errorf("wilson %v %v", chance.CILow, chance.CIHigh)
	}
	if ppm.Kind != "ppm" || math.Abs(ppm.Value-4) > 1e-9 || ppm.CILow >= ppm.Value || ppm.CIHigh <= ppm.Value {
		t.Errorf("ppm %+v", ppm)
	}
	if _, _, err := ProcRate(1, ProcCounts{}, 0.95); !errors.Is(err, ErrNoTriggers) {
		t.Errorf("err = %v", err)
	}
	if z := zScore(0.95); math.Abs(z-1.95996) > 1e-4 {
		t.Errorf("z %v", z)
	}
}

func TestCountProcsFromFixture(t *testing.T) {
	res, err := ingest.ParseFileOptions("../testdata/fixture.txt", ingest.ParseOptions{Year: 2026, Location: time.UTC})
	if err != nil {
		t.Fatal(err)
	}
	c := CountProcs(res, 12880, []string{"SWING_DAMAGE", "SWING_MISSED"}, "Player-4372-0000BBBB", "fixture")
	if c.Triggers != 6 || c.Procs != 2 || c.DurationMs != 33500 {
		t.Errorf("counts %+v", c)
	}
}

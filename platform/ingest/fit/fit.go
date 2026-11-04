// Package fit estimates spell coefficients and proc rates from parsed logs.
package fit

import (
	"errors"
	"math"
	"math/rand"
	"sort"

	"github.com/Ohjeezoos/simforever-companion/platform/ingest"
)

// Measurement mirrors the measurements table of the platform contract.
type Measurement struct {
	SpellID int32    `json:"spell_id"`
	Kind    string   `json:"kind"`
	Value   float64  `json:"value"`
	CILow   float64  `json:"ci_low"`
	CIHigh  float64  `json:"ci_high"`
	N       int      `json:"n"`
	Method  string   `json:"method"`
	Sources []string `json:"sources"`
}

type Sample struct {
	SpellPower float64 `json:"spellPower"`
	Damage     float64 `json:"damage"`
	Source     string  `json:"source"`
}

type CoefOptions struct {
	Bootstrap  int     // resamples; 0 means 2000
	Seed       int64   // RNG seed for the bootstrap
	Confidence float64 // 0 means 0.95
}

var ErrNoSpread = errors.New("fit: samples span fewer than two distinct spell power values; swap spell power gear between fights")
var ErrTooFew = errors.New("fit: need at least three samples")

// CollectSamples gathers clean direct hits of spellID: SPELL_DAMAGE that is
// not a crit and has no resist, block, absorb, glancing or crushing, with the
// caster's spell power taken from advanced logging on that event, or from a
// spellPower key in the player's stats.
func CollectSamples(res *ingest.Result, spellID int32, source string) []Sample {
	var out []Sample
	for fi := range res.Fights {
		f := &res.Fights[fi]
		sp := map[string]float64{}
		for _, p := range f.Players {
			if v, ok := numeric(p.Stats["spellPower"]); ok {
				sp[p.GUID] = v
			}
		}
		for i := range f.Events {
			ev := &f.Events[i]
			if ev.Event != "SPELL_DAMAGE" || ev.SpellID != spellID {
				continue
			}
			if ev.Critical || ev.Resisted != 0 || ev.Blocked != 0 || ev.Absorbed != 0 || ev.Glancing || ev.Crushing {
				continue
			}
			power, ok := 0.0, false
			if g, _ := ev.Extra["advGuid"].(string); g != "" && g == ev.SourceGUID {
				power, ok = numeric(ev.Extra["spellPower"])
			}
			if !ok {
				power, ok = sp[ev.SourceGUID]
			}
			if !ok {
				continue
			}
			out = append(out, Sample{SpellPower: power, Damage: float64(ev.Amount), Source: source})
		}
	}
	return out
}

func numeric(v any) (float64, bool) {
	switch n := v.(type) {
	case int64:
		return float64(n), true
	case int:
		return float64(n), true
	case float64:
		return n, true
	}
	return 0, false
}

// Coefficient fits damage = base + coef * spellPower by ordinary least
// squares and reports percentile bootstrap intervals for both.
func Coefficient(spellID int32, samples []Sample, opts CoefOptions) (coef, base Measurement, err error) {
	if opts.Bootstrap <= 0 {
		opts.Bootstrap = 2000
	}
	if opts.Confidence <= 0 || opts.Confidence >= 1 {
		opts.Confidence = 0.95
	}
	if len(samples) < 3 {
		return coef, base, ErrTooFew
	}
	if distinct(samples) < 2 {
		return coef, base, ErrNoSpread
	}
	slope, icept, ok := ols(samples)
	if !ok {
		return coef, base, ErrNoSpread
	}
	rng := rand.New(rand.NewSource(opts.Seed))
	slopes := make([]float64, 0, opts.Bootstrap)
	icepts := make([]float64, 0, opts.Bootstrap)
	draw := make([]Sample, len(samples))
	for b := 0; b < opts.Bootstrap; b++ {
		for i := range draw {
			draw[i] = samples[rng.Intn(len(samples))]
		}
		s, c, ok := ols(draw)
		if !ok {
			continue
		}
		slopes = append(slopes, s)
		icepts = append(icepts, c)
	}
	sources := uniqueSources(samples)
	coef = Measurement{SpellID: spellID, Kind: "coefficient", Value: slope, N: len(samples), Method: "ols_bootstrap", Sources: sources}
	base = Measurement{SpellID: spellID, Kind: "base_damage", Value: icept, N: len(samples), Method: "ols_bootstrap", Sources: sources}
	coef.CILow, coef.CIHigh = percentiles(slopes, opts.Confidence)
	base.CILow, base.CIHigh = percentiles(icepts, opts.Confidence)
	return coef, base, nil
}

func distinct(s []Sample) int {
	seen := map[float64]bool{}
	for _, x := range s {
		seen[x.SpellPower] = true
	}
	return len(seen)
}

func ols(s []Sample) (slope, icept float64, ok bool) {
	n := float64(len(s))
	var sx, sy float64
	for _, p := range s {
		sx += p.SpellPower
		sy += p.Damage
	}
	mx, my := sx/n, sy/n
	var sxx, sxy float64
	for _, p := range s {
		dx := p.SpellPower - mx
		sxx += dx * dx
		sxy += dx * (p.Damage - my)
	}
	if sxx == 0 {
		return 0, 0, false
	}
	slope = sxy / sxx
	return slope, my - slope*mx, true
}

func percentiles(v []float64, conf float64) (lo, hi float64) {
	if len(v) == 0 {
		return math.NaN(), math.NaN()
	}
	sort.Float64s(v)
	alpha := (1 - conf) / 2
	at := func(q float64) float64 {
		i := int(math.Round(q * float64(len(v)-1)))
		return v[max(0, min(i, len(v)-1))]
	}
	return at(alpha), at(1 - alpha)
}

func uniqueSources(s []Sample) []string {
	seen := map[string]bool{}
	var out []string
	for _, x := range s {
		if x.Source != "" && !seen[x.Source] {
			seen[x.Source] = true
			out = append(out, x.Source)
		}
	}
	sort.Strings(out)
	if out == nil {
		out = []string{}
	}
	return out
}

package fit

import (
	"errors"
	"math"

	"github.com/Ohjeezoos/simforever-companion/platform/ingest"
)

type ProcCounts struct {
	Triggers   int      `json:"triggers"`
	Procs      int      `json:"procs"`
	DurationMs int64    `json:"durationMs"`
	Sources    []string `json:"sources"`
}

var ErrNoTriggers = errors.New("fit: no trigger events found")

// CountProcs counts aura applications and refreshes of auraID gained by each
// player, against that player's trigger events (for example SWING_DAMAGE and
// SWING_MISSED for a weapon proc, SPELL_CAST_SUCCESS for a spell proc), over
// the summed fight duration. Filter by sourceGUID when non-empty.
func CountProcs(res *ingest.Result, auraID int32, triggerEvents []string, sourceGUID, source string) ProcCounts {
	trig := map[string]bool{}
	for _, t := range triggerEvents {
		trig[t] = true
	}
	c := ProcCounts{Sources: []string{}}
	if source != "" {
		c.Sources = []string{source}
	}
	for fi := range res.Fights {
		f := &res.Fights[fi]
		c.DurationMs += f.DurationMs
		for i := range f.Events {
			ev := &f.Events[i]
			if sourceGUID != "" && ev.SourceGUID != sourceGUID {
				continue
			}
			switch {
			case trig[ev.Event]:
				c.Triggers++
			case ev.SpellID == auraID && ev.SourceGUID == ev.TargetGUID && (ev.Event == "SPELL_AURA_APPLIED" || ev.Event == "SPELL_AURA_REFRESH"):
				c.Procs++
			}
		}
	}
	return c
}

// ProcRate returns proc chance per trigger with a Wilson score interval and
// procs per minute with that interval scaled by the trigger rate.
func ProcRate(auraID int32, c ProcCounts, confidence float64) (ppm, chance Measurement, err error) {
	if c.Triggers == 0 {
		return ppm, chance, ErrNoTriggers
	}
	if confidence <= 0 || confidence >= 1 {
		confidence = 0.95
	}
	z := zScore(confidence)
	p, lo, hi := wilson(c.Procs, c.Triggers, z)
	chance = Measurement{SpellID: auraID, Kind: "proc_chance", Value: p, CILow: lo, CIHigh: hi, N: c.Triggers, Method: "wilson", Sources: c.Sources}
	minutes := float64(c.DurationMs) / 60000
	if minutes > 0 {
		rate := float64(c.Triggers) / minutes
		ppm = Measurement{SpellID: auraID, Kind: "ppm", Value: p * rate, CILow: lo * rate, CIHigh: hi * rate, N: c.Triggers, Method: "wilson", Sources: c.Sources}
	}
	return ppm, chance, nil
}

func wilson(k, n int, z float64) (p, lo, hi float64) {
	nf := float64(n)
	p = float64(k) / nf
	z2 := z * z
	denom := 1 + z2/nf
	center := (p + z2/(2*nf)) / denom
	half := z * math.Sqrt(p*(1-p)/nf+z2/(4*nf*nf)) / denom
	return p, math.Max(0, center-half), math.Min(1, center+half)
}

// zScore inverts the normal CDF for the two-sided confidence level by bisection.
func zScore(conf float64) float64 {
	target := 1 - (1-conf)/2
	lo, hi := 0.0, 10.0
	for i := 0; i < 100; i++ {
		mid := (lo + hi) / 2
		if 0.5*(1+math.Erf(mid/math.Sqrt2)) < target {
			lo = mid
		} else {
			hi = mid
		}
	}
	return (lo + hi) / 2
}

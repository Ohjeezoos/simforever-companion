package probe

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// KindRatingPerPercent is the measurement kind for a rating to percent
// conversion: how much rating one percent costs at one character level.
const KindRatingPerPercent = "rating_per_percent"

// ErrRatingConflict is returned for a rating key whose readings disagree. A
// conversion is a constant per level, so disagreeing readings are a fault to
// report, never something to average away.
var ErrRatingConflict = errors.New("probe: rating readings disagree")

// RatingReading is one rating amount and the percent the client reported for it.
type RatingReading struct {
	Rating     float64 `json:"rating"`
	Percent    float64 `json:"percent"`
	PerPercent float64 `json:"ratingPerPercent"`
	At         int64   `json:"at"`
}

// RatingFit is the conversion derived for one rating key at one level.
type RatingFit struct {
	Key        string          `json:"key"`
	Level      int32           `json:"level"`
	PerPercent float64         `json:"ratingPerPercent"`
	Min        float64         `json:"min"`
	Max        float64         `json:"max"`
	Spread     float64         `json:"spread"`
	N          int             `json:"n"`
	Verified   bool            `json:"verified"`
	Conflict   string          `json:"conflict,omitempty"`
	Readings   []RatingReading `json:"readings"`
}

// Err reports a conflict as an error, so callers cannot store a disputed row
// by accident.
func (f RatingFit) Err() error {
	if f.Conflict == "" {
		return nil
	}
	return fmt.Errorf("%w: %s", ErrRatingConflict, f.Conflict)
}

type RatingOptions struct {
	// Tolerance is the relative spread two readings may have and still count
	// as the same constant. 0 means 0.01.
	Tolerance float64
	// MinReadings is how many agreeing readings mark the row verified. 0 means 3.
	MinReadings int
}

func (o RatingOptions) tolerance() float64 {
	if o.Tolerance <= 0 {
		return 0.01
	}
	return o.Tolerance
}

func (o RatingOptions) minReadings() int {
	if o.MinReadings <= 0 {
		return 3
	}
	return o.MinReadings
}

// FitRatings derives rating per one percent for every rating key and level in
// the export. The addon already deduplicates by rating amount, so each reading
// in a group is an independent observation of the same constant: the value is
// taken from the largest rating amount, which carries the least rounding, and
// the group is rejected when the readings do not agree.
func FitRatings(exp *Export, opts RatingOptions) []RatingFit {
	groups := map[string][]RatingReading{}
	levels := map[string]int32{}
	keys := map[string]string{}
	for key, bucket := range exp.Ratings {
		for amount, r := range bucket {
			rating, err := strconv.ParseFloat(amount, 64)
			if err != nil || rating <= 0 || r.Percent <= 0 {
				continue
			}
			level := r.Level
			if level == 0 {
				level = exp.Character.Level
			}
			g := key + "@" + strconv.Itoa(int(level))
			groups[g] = append(groups[g], RatingReading{Rating: rating, Percent: r.Percent, PerPercent: rating / r.Percent, At: r.At})
			levels[g], keys[g] = level, key
		}
	}
	out := make([]RatingFit, 0, len(groups))
	for g, readings := range groups {
		sort.Slice(readings, func(i, j int) bool { return readings[i].Rating < readings[j].Rating })
		f := RatingFit{Key: keys[g], Level: levels[g], N: len(readings), Readings: readings}
		f.Min, f.Max = readings[0].PerPercent, readings[0].PerPercent
		for _, r := range readings {
			f.Min, f.Max = math.Min(f.Min, r.PerPercent), math.Max(f.Max, r.PerPercent)
		}
		f.PerPercent = readings[len(readings)-1].PerPercent
		if f.PerPercent > 0 {
			f.Spread = (f.Max - f.Min) / f.PerPercent
		}
		if f.Spread > opts.tolerance() {
			f.Conflict = conflictMessage(f, opts.tolerance())
		}
		f.Verified = f.Conflict == "" && f.N >= opts.minReadings()
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Key != out[j].Key {
			return out[i].Key < out[j].Key
		}
		return out[i].Level < out[j].Level
	})
	return out
}

func conflictMessage(f RatingFit, tol float64) string {
	var parts []string
	for _, r := range f.Readings {
		parts = append(parts, fmt.Sprintf("%g rating = %g%% (%g per percent)", r.Rating, r.Percent, round(r.PerPercent, 6)))
	}
	return fmt.Sprintf("%s at level %d: %d readings span %g to %g rating per percent, %.2f%% apart, more than the %.2f%% allowed [%s]",
		f.Key, f.Level, f.N, round(f.Min, 6), round(f.Max, 6), f.Spread*100, tol*100, strings.Join(parts, "; "))
}

func round(v float64, places int) float64 {
	p := math.Pow(10, float64(places))
	return math.Round(v*p) / p
}

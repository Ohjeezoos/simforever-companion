package probe

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/Ohjeezoos/simforever-companion/platform/ingest/fit"
	"github.com/Ohjeezoos/simforever-companion/platform/store"
)

type Options struct {
	Rating RatingOptions
	Coef   fit.CoefOptions
	// ItemTolerance is the absolute difference two stat values may have and
	// still count as the same. 0 means 0.01.
	ItemTolerance float64
	// Source labels the readings in the stored sources, for example the file
	// name or the submitting address.
	Source string
}

// Run is everything one export measured.
type Run struct {
	Export       *Export     `json:"-"`
	Ratings      []RatingFit `json:"ratings"`
	Coefficients []CoefFit   `json:"coefficients"`
	CoefSkipped  []CoefSkip  `json:"coefficientsSkipped"`
	Items        ItemReport  `json:"items"`
	// Talents is what one point in a talent was measured to be worth. The
	// client states nothing about this, so it is the only source there is.
	Talents []TalentFit `json:"talents"`
	Source  string      `json:"source,omitempty"`
}

// Analyze runs every fit the export supports. ref may be nil, which skips the
// item check but leaves the rest.
func Analyze(exp *Export, ref map[int32]Reference, opts Options) *Run {
	r := &Run{Export: exp, Source: opts.Source,
		Ratings: FitRatings(exp, opts.Rating), Items: ItemReport{NotInReference: []int32{}, Items: []ItemCheck{}, Stats: []StatSummary{}}}
	r.Coefficients, r.CoefSkipped = FitCoefficients(exp, opts.Coef, opts.Source)
	r.Talents = FitTalents(exp.Snapshots)
	if ref != nil {
		r.Items = CheckItems(exp, ref, opts.ItemTolerance)
	}
	return r
}

// Method names the addon version that produced a reading, so a later addon
// change can never be mistaken for a client change.
func (r *Run) Method() string {
	v := r.Export.Version
	if v == "" {
		v = "unknown"
	}
	return "simforever_probe/" + v
}

func (r *Run) character() map[string]any {
	c := r.Export.Character
	return map[string]any{"name": c.Name, "realm": c.Realm, "guid": c.GUID, "class": c.Class,
		"race": c.Race, "level": c.Level, "faction": c.Faction}
}

// Conflicts lists the rating groups whose readings disagree. They are never stored.
func (r *Run) Conflicts() []RatingFit {
	out := []RatingFit{}
	for _, f := range r.Ratings {
		if f.Conflict != "" {
			out = append(out, f)
		}
	}
	return out
}

// Rows turns the consistent fits into measurement rows. Rating conflicts are
// left out on purpose: a disputed conversion is an error to fix, not a number
// to publish.
func (r *Run) Rows(gameID int32, buildID *int32) []store.NewMeasurement {
	rows := []store.NewMeasurement{}
	for _, f := range r.Ratings {
		if f.Conflict != "" {
			continue
		}
		sources, _ := json.Marshal(map[string]any{
			"probe":     map[string]any{"addonVersion": r.Export.Version, "clientBuild": r.Export.Build.String(), "source": r.Source},
			"character": r.character(),
			"ratingKey": f.Key,
			"level":     f.Level,
			"readings":  f.Readings,
		})
		lo, hi := f.Min, f.Max
		rows = append(rows, store.NewMeasurement{
			GameVersionID: gameID, BuildID: buildID, SpellID: 0, Kind: KindRatingPerPercent,
			Value: f.PerPercent, CILow: &lo, CIHigh: &hi, N: int32(f.N), Method: r.Method(),
			Sources: sources, Verified: f.Verified,
			Notes: fmt.Sprintf("%s: %g rating per 1%% at level %d", f.Key, round(f.PerPercent, 6), f.Level),
		})
	}
	for _, c := range r.Coefficients {
		for _, m := range []fit.Measurement{c.Coefficient, c.Base} {
			sources, _ := json.Marshal(map[string]any{
				"probe":     map[string]any{"addonVersion": r.Export.Version, "clientBuild": r.Export.Build.String(), "source": r.Source},
				"character": r.character(),
				"sources":   m.Sources,
				"spellName": c.SpellName,
				"samples":   c.Samples,
			})
			lo, hi := m.CILow, m.CIHigh
			rows = append(rows, store.NewMeasurement{
				GameVersionID: gameID, BuildID: buildID, SpellID: m.SpellID, Kind: m.Kind,
				Value: m.Value, CILow: &lo, CIHigh: &hi, N: int32(m.N), Method: m.Method,
				Sources: sources, Verified: false,
				Notes: fmt.Sprintf("%s from %d probe events", c.SpellName, c.Samples),
			})
		}
	}
	return rows
}

// Summary is what a caller reports back: counts and the fits, without the
// per-item detail.
type Summary struct {
	AddonVersion    string         `json:"addonVersion"`
	ClientBuild     string         `json:"clientBuild"`
	Character       map[string]any `json:"character"`
	Counts          Counts         `json:"counts"`
	Ratings         []RatingFit    `json:"ratings"`
	RatingConflicts []RatingFit    `json:"ratingConflicts"`
	Coefficients    []CoefFit      `json:"coefficients"`
	CoefSkipped     []CoefSkip     `json:"coefficientsSkipped"`
	Items           ItemSummary    `json:"items"`
	Talents         []TalentFit    `json:"talents"`
	TalentConflicts []TalentFit    `json:"talentConflicts"`
	Hidden          []string       `json:"hidden"`
}

type Counts struct {
	Snapshots      int `json:"snapshots"`
	Events         int `json:"events"`
	Items          int `json:"items"`
	Targets        int `json:"targets"`
	RatingReadings int `json:"ratingReadings"`
}

type ItemSummary struct {
	Checked            int           `json:"checked"`
	Matched            int           `json:"matched"`
	Mismatched         int           `json:"mismatched"`
	OnlyClient         int           `json:"onlyClient"`
	OnlyPlatform       int           `json:"onlyPlatform"`
	RatingsNotCompared int           `json:"ratingsNotCompared"`
	NotInReference     []int32       `json:"notInReference"`
	Stats              []StatSummary `json:"stats"`
}

func (r *Run) Summary() Summary {
	readings := 0
	for _, f := range r.Ratings {
		readings += f.N
	}
	agreed := []RatingFit{}
	for _, f := range r.Ratings {
		if f.Conflict == "" {
			agreed = append(agreed, f)
		}
	}
	agreedTalents, disputedTalents := []TalentFit{}, []TalentFit{}
	for _, f := range r.Talents {
		if f.Conflict == "" {
			agreedTalents = append(agreedTalents, f)
		} else {
			disputedTalents = append(disputedTalents, f)
		}
	}
	return Summary{
		AddonVersion: r.Export.Version,
		ClientBuild:  r.Export.Build.String(),
		Character:    r.character(),
		Counts: Counts{Snapshots: len(r.Export.Snapshots), Events: len(r.Export.Events), Items: len(r.Export.Items),
			Targets: len(r.Export.Targets), RatingReadings: readings},
		Ratings:         agreed,
		RatingConflicts: r.Conflicts(),
		Coefficients:    r.Coefficients,
		CoefSkipped:     r.CoefSkipped,
		Talents:         agreedTalents,
		TalentConflicts: disputedTalents,
		Items: ItemSummary{Checked: r.Items.Checked, Matched: r.Items.Matched, Mismatched: r.Items.Mismatched,
			OnlyClient: r.Items.OnlyClient, OnlyPlatform: r.Items.OnlyPlatform, RatingsNotCompared: r.Items.RatingsNotCompared,
			NotInReference: r.Items.NotInReference, Stats: r.Items.Stats},
		Hidden: r.Hidden(),
	}
}

// Hidden lists what this client kept from the addon, which is the difference
// between a reading that is missing and a reading that cannot exist.
func (r *Run) Hidden() []string {
	out := []string{}
	for name, slots := range r.Export.Secrets {
		combat, rest := false, false
		for _, when := range slots {
			combat = combat || bool(when.Combat)
			rest = rest || bool(when.Rest)
		}
		switch {
		case combat && !rest:
			out = append(out, name+" (in combat)")
		case combat || rest:
			out = append(out, name)
		}
	}
	for name, call := range r.Export.Blocked {
		if !bool(call.Named) && len(call.LastCalls) > 0 {
			out = append(out, "a refused action after "+strings.Join(call.LastCalls, ", "))
			continue
		}
		out = append(out, name+" (refused)")
	}
	sort.Strings(out)
	return out
}

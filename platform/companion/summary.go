package companion

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode"

	"github.com/Ohjeezoos/simforever-companion/platform/probe"
)

// Headline is the single line every upload prints.
func Headline(s probe.Summary, r *Result) string {
	verb := "would upload"
	if r != nil {
		verb = "uploaded"
	}
	parts := []string{
		plural(len(s.Ratings), "rating conversion", "rating conversions"),
		plural(len(s.Coefficients), "spell coefficient", "spell coefficients"),
		fmt.Sprintf("%d items checked", s.Items.Checked),
	}
	if len(s.RatingConflicts) > 0 {
		parts = append(parts, fmt.Sprintf("%d disagreed", len(s.RatingConflicts)))
	}
	if r != nil {
		parts = append(parts, fmt.Sprintf("%d measurements stored", len(r.MeasurementIDs)))
	}
	line := fmt.Sprintf("%s %s: %s", verb, character(s.Character), strings.Join(parts, ", "))
	if r != nil && r.UploadID != 0 {
		line += fmt.Sprintf(" (upload %d)", r.UploadID)
	}
	return line
}

// WriteSummary prints the headline and then, in plain words, what the platform
// actually measured from this export.
func WriteSummary(w io.Writer, s probe.Summary, r *Result) {
	fmt.Fprintln(w, Headline(s, r))
	build := s.ClientBuild
	if r != nil && r.Build != "" {
		build = r.Build
	}
	fmt.Fprintf(w, "  recorded by addon %s on client %s: %d snapshots, %d combat events, %d items, %d targets\n",
		orUnknown(s.AddonVersion), orUnknown(build), s.Counts.Snapshots, s.Counts.Events, s.Counts.Items, s.Counts.Targets)

	if len(s.Ratings) == 0 {
		fmt.Fprintln(w, "  rating conversions: none yet (swap gear that carries ratings, so several different amounts are seen)")
	} else {
		fmt.Fprintf(w, "  rating conversions (%d readings):\n", s.Counts.RatingReadings)
		for _, f := range s.Ratings {
			note := ""
			if f.Verified {
				note = ", agreed by enough readings to be trusted"
			}
			fmt.Fprintf(w, "    %s at level %d: %s rating per 1%% (from %s%s)\n",
				words(f.Key), f.Level, num(f.PerPercent), plural(f.N, "reading", "readings"), note)
		}
	}
	for _, f := range s.RatingConflicts {
		fmt.Fprintf(w, "    %s at level %d disagreed, so it was not stored: %s\n", words(f.Key), f.Level, f.Conflict)
	}

	if len(s.Coefficients) > 0 {
		fmt.Fprintln(w, "  spell coefficients fitted:")
		for _, c := range s.Coefficients {
			fmt.Fprintf(w, "    %s (%d): %s per point of spell power, base %s, from %s\n",
				orUnknown(c.SpellName), c.SpellID, num(c.Coefficient.Value), num(c.Base.Value),
				plural(c.Samples, "cast", "casts"))
		}
	}
	if len(s.CoefSkipped) > 0 && len(s.Coefficients) == 0 {
		fmt.Fprintln(w, "  spell coefficients: none fitted yet")
	}
	for _, c := range s.CoefSkipped {
		fmt.Fprintf(w, "    %s (%d) not fitted from %s: %s\n", orUnknown(c.SpellName), c.SpellID,
			plural(c.Samples, "cast", "casts"), c.Reason)
	}

	if s.Items.Checked > 0 {
		fmt.Fprintf(w, "  items: %d checked against the platform, %d matched, %d differed, %d only the client had, %d only the platform had\n",
			s.Items.Checked, s.Items.Matched, s.Items.Mismatched, s.Items.OnlyClient, s.Items.OnlyPlatform)
		for _, st := range s.Items.Stats {
			if st.Mismatched == 0 && st.OnlyClient == 0 && st.OnlyPlatform == 0 {
				continue
			}
			fmt.Fprintf(w, "    %s: %d matched, %d differed, %d only client, %d only platform\n",
				words(st.Stat), st.Matched, st.Mismatched, st.OnlyClient, st.OnlyPlatform)
		}
	} else if r != nil {
		fmt.Fprintln(w, "  items: none could be checked, the platform has no data for this build yet")
	} else {
		fmt.Fprintln(w, "  items: checked by the platform on upload, not locally")
	}
}

func character(m map[string]any) string {
	name := text(m, "name")
	if name == "" {
		name = "unknown character"
	}
	if realm := text(m, "realm"); realm != "" {
		name += "-" + realm
	}
	var extra []string
	if c := text(m, "class"); c != "" {
		extra = append(extra, titleWord(c))
	}
	if l := text(m, "level"); l != "" && l != "0" {
		extra = append(extra, "level "+l)
	}
	if len(extra) > 0 {
		name += " (" + strings.Join(extra, " ") + ")"
	}
	return name
}

func text(m map[string]any, k string) string {
	v, ok := m[k]
	if !ok || v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

func titleWord(s string) string {
	if s == "" {
		return s
	}
	r := []rune(strings.ToLower(s))
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// words turns the addon's camelCase keys into something a player reads. A run
// of capitals stays one word, so weaponDPS reads as "weapon dps".
func words(key string) string {
	r := []rune(key)
	var b strings.Builder
	for i, c := range r {
		if i > 0 && unicode.IsUpper(c) && (!unicode.IsUpper(r[i-1]) || (i+1 < len(r) && unicode.IsLower(r[i+1]))) {
			b.WriteByte(' ')
		}
		b.WriteRune(unicode.ToLower(c))
	}
	if b.Len() == 0 {
		return "unnamed"
	}
	return b.String()
}

func num(f float64) string { return strconv.FormatFloat(f, 'g', 6, 64) }

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}

func orUnknown(s string) string {
	if strings.TrimSpace(s) == "" {
		return "unknown"
	}
	return s
}

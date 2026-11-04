package ingest

import (
	"errors"
	"strconv"
	"strings"
	"time"
)

// A log line is "M/D HH:MM:SS.mmm" followed by two spaces and the payload.
// Newer clients write "M/D/YYYY HH:MM:SS.mmm-Z" with a UTC offset in hours.
func splitLine(line string) (ts, payload string, ok bool) {
	i := strings.Index(line, "  ")
	if i < 0 {
		return "", "", false
	}
	return line[:i], strings.TrimLeft(line[i+2:], " "), true
}

type stamp struct {
	month, day, year int
	clock            time.Duration
}

func parseTimestamp(s string) (stamp, error) {
	var st stamp
	date, clock, ok := strings.Cut(s, " ")
	if !ok {
		return st, errors.New("bad timestamp")
	}
	parts := strings.Split(date, "/")
	if len(parts) < 2 || len(parts) > 3 {
		return st, errors.New("bad date")
	}
	var err error
	if st.month, err = strconv.Atoi(parts[0]); err != nil || st.month < 1 || st.month > 12 {
		return st, errors.New("bad month")
	}
	if st.day, err = strconv.Atoi(parts[1]); err != nil || st.day < 1 || st.day > 31 {
		return st, errors.New("bad day")
	}
	if len(parts) == 3 {
		if st.year, err = strconv.Atoi(parts[2]); err != nil {
			return st, errors.New("bad year")
		}
	}
	if i := strings.IndexAny(clock, "+-"); i > 0 {
		clock = clock[:i]
	}
	hms := strings.Split(clock, ":")
	if len(hms) != 3 {
		return st, errors.New("bad clock")
	}
	h, err1 := strconv.Atoi(hms[0])
	m, err2 := strconv.Atoi(hms[1])
	sec, err3 := strconv.ParseFloat(hms[2], 64)
	if err1 != nil || err2 != nil || err3 != nil {
		return st, errors.New("bad clock")
	}
	st.clock = time.Duration(h)*time.Hour + time.Duration(m)*time.Minute + time.Duration(sec*float64(time.Second))
	return st, nil
}

// splitFields splits on commas outside quotes and outside ()/[] groups, so
// COMBATANT_INFO arrays come back as single raw fields. Quotes are stripped.
func splitFields(s string) []string {
	out := make([]string, 0, 32)
	var b strings.Builder
	depth := 0
	quoted := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quoted:
			if c == '"' {
				quoted = false
			} else {
				b.WriteByte(c)
			}
		case c == '"':
			quoted = true
		case c == '(' || c == '[':
			depth++
			b.WriteByte(c)
		case c == ')' || c == ']':
			if depth > 0 {
				depth--
			}
			b.WriteByte(c)
		case c == ',' && depth == 0:
			out = append(out, b.String())
			b.Reset()
		default:
			b.WriteByte(c)
		}
	}
	out = append(out, b.String())
	return out
}

// splitGroup parses "[a,(b,c),d]" into its top-level items, brackets removed.
func splitGroup(s string) []string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && (s[0] == '[' || s[0] == '(') {
		s = s[1 : len(s)-1]
	}
	if strings.TrimSpace(s) == "" {
		return []string{}
	}
	return splitFields(s)
}

func atoi(s string) int64 {
	if s == "" || s == "nil" {
		return 0
	}
	if strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X") {
		v, err := strconv.ParseInt(s[2:], 16, 64)
		if err != nil {
			u, err := strconv.ParseUint(s[2:], 16, 64)
			if err != nil {
				return 0
			}
			return int64(u)
		}
		return v
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return 0
		}
		return int64(f)
	}
	return v
}

func atof(s string) float64 {
	if s == "" || s == "nil" {
		return 0
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return float64(atoi(s))
	}
	return f
}

func atob(s string) bool {
	return s == "1" || s == "true"
}

func nilStr(s string) string {
	if s == "nil" || s == "0000000000000000" {
		return ""
	}
	return s
}

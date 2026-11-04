package ingest

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

type ParseOptions struct {
	Year       int            // year of the first line; 0 means the current year
	GapSeconds float64        // combat gap that closes a fight outside encounters; 0 means 20
	Location   *time.Location // zone the log was written in; nil means time.Local
}

// ParseFile parses one combat log file. Fights are delimited by
// ENCOUNTER_START/ENCOUNTER_END when present, otherwise by combat gaps.
func ParseFile(path string) (*Result, error) { return ParseFileOptions(path, ParseOptions{}) }

func ParseFileOptions(path string, opts ParseOptions) (*Result, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Parse(f, opts)
}

func Parse(r io.Reader, opts ParseOptions) (*Result, error) {
	if opts.Year == 0 {
		opts.Year = time.Now().Year()
	}
	if opts.GapSeconds <= 0 {
		opts.GapSeconds = 20
	}
	if opts.Location == nil {
		opts.Location = time.Local
	}
	res := &Result{}
	seg := newSegmenter(time.Duration(opts.GapSeconds * float64(time.Second)))
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	year, lastMonth := opts.Year, 0
	// The file says which layout it was written in on its first line, and
	// nothing before that line is a damage event.
	ver := logVersion(0)
	for sc.Scan() {
		res.Lines++
		line := strings.TrimRight(sc.Text(), "\r")
		if res.Lines == 1 {
			line = strings.TrimPrefix(line, "\xef\xbb\xbf")
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		warn := func(msg string) {
			res.Warnings = append(res.Warnings, fmt.Sprintf("line %d: %s", res.Lines, msg))
		}
		ts, payload, ok := splitLine(line)
		if !ok {
			warn("no timestamp separator")
			continue
		}
		st, err := parseTimestamp(ts)
		if err != nil {
			warn(err.Error())
			continue
		}
		if st.year != 0 {
			year = st.year
		} else if lastMonth != 0 && st.month < lastMonth {
			year++
		}
		lastMonth = st.month
		t := time.Date(year, time.Month(st.month), st.day, 0, 0, 0, 0, opts.Location).Add(st.clock)
		fields := splitFields(payload)
		if fields[0] == "" {
			warn("empty event name")
			continue
		}
		if fields[0] == "COMBAT_LOG_VERSION" && len(fields) > 1 {
			ver = logVersion(atoi(fields[1]))
		}
		ev, err := decodeEventVersion(t, fields, ver)
		if err != nil {
			warn(err.Error())
			if ev.SourceGUID == "" && ev.TargetGUID == "" {
				continue
			}
		}
		if ev.combat() {
			res.DamageLines++
		}
		seg.add(ev, warn)
	}
	if err := sc.Err(); err != nil {
		return res, err
	}
	res.Fights = seg.finish(func(msg string) { res.Warnings = append(res.Warnings, msg) })
	res.Levels = seg.levels()
	return res, nil
}

package probe

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// luaParser reads the subset of Lua that WoW's SavedVariables writer emits:
// top level `NAME = value` assignments of nested table literals, strings,
// numbers, booleans and nil, with line and long comments.
type luaParser struct {
	s string
	i int
}

// parseLuaChunk returns the value assigned to global, ignoring every other
// assignment in the file.
func parseLuaChunk(src, global string) (any, error) {
	p := &luaParser{s: src}
	var out any
	found := false
	for {
		p.skip()
		if p.i >= len(p.s) {
			break
		}
		name := p.ident()
		if name == "" {
			return nil, p.errf("expected a global assignment")
		}
		p.skip()
		if !p.take('=') {
			return nil, p.errf("expected = after %s", name)
		}
		v, err := p.value()
		if err != nil {
			return nil, err
		}
		if name == global {
			out, found = v, true
		}
		p.skip()
		p.take(';')
	}
	if !found {
		return nil, fmt.Errorf("probe: %s is not assigned in this file", global)
	}
	return out, nil
}

func (p *luaParser) errf(format string, args ...any) error {
	line := 1 + strings.Count(p.s[:min(p.i, len(p.s))], "\n")
	return fmt.Errorf("probe: lua line %d: %s", line, fmt.Sprintf(format, args...))
}

func (p *luaParser) take(c byte) bool {
	if p.i < len(p.s) && p.s[p.i] == c {
		p.i++
		return true
	}
	return false
}

func (p *luaParser) skip() {
	for p.i < len(p.s) {
		switch c := p.s[p.i]; {
		case c == ' ' || c == '\t' || c == '\r' || c == '\n':
			p.i++
		case strings.HasPrefix(p.s[p.i:], "--"):
			p.i += 2
			if lvl, ok := p.longBracket(); ok {
				p.longString(lvl)
				continue
			}
			for p.i < len(p.s) && p.s[p.i] != '\n' {
				p.i++
			}
		default:
			return
		}
	}
}

func isIdent(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

func (p *luaParser) ident() string {
	start := p.i
	if p.i < len(p.s) && (p.s[p.i] >= '0' && p.s[p.i] <= '9') {
		return ""
	}
	for p.i < len(p.s) && isIdent(p.s[p.i]) {
		p.i++
	}
	return p.s[start:p.i]
}

// longBracket consumes an opening [==[ and returns the number of equals signs.
func (p *luaParser) longBracket() (int, bool) {
	j := p.i
	if j >= len(p.s) || p.s[j] != '[' {
		return 0, false
	}
	j++
	lvl := 0
	for j < len(p.s) && p.s[j] == '=' {
		lvl++
		j++
	}
	if j >= len(p.s) || p.s[j] != '[' {
		return 0, false
	}
	p.i = j + 1
	return lvl, true
}

func (p *luaParser) longString(lvl int) string {
	closer := "]" + strings.Repeat("=", lvl) + "]"
	if end := strings.Index(p.s[p.i:], closer); end >= 0 {
		out := p.s[p.i : p.i+end]
		p.i += end + len(closer)
		return strings.TrimPrefix(out, "\n")
	}
	out := p.s[p.i:]
	p.i = len(p.s)
	return out
}

func (p *luaParser) value() (any, error) {
	p.skip()
	if p.i >= len(p.s) {
		return nil, p.errf("unexpected end of file")
	}
	switch c := p.s[p.i]; {
	case c == '{':
		return p.table()
	case c == '"' || c == '\'':
		return p.quoted()
	case c == '[':
		if lvl, ok := p.longBracket(); ok {
			return p.longString(lvl), nil
		}
		return nil, p.errf("unexpected [")
	case c == '-' || c == '.' || c >= '0' && c <= '9':
		return p.number()
	}
	switch id := p.ident(); id {
	case "true":
		return true, nil
	case "false":
		return false, nil
	case "nil":
		return nil, nil
	case "inf":
		return math.Inf(1), nil
	case "nan":
		return math.NaN(), nil
	case "":
		return nil, p.errf("unexpected %q", p.s[p.i])
	default:
		return nil, p.errf("unexpected value %s", id)
	}
}

func (p *luaParser) quoted() (string, error) {
	quote := p.s[p.i]
	p.i++
	var b strings.Builder
	for p.i < len(p.s) {
		c := p.s[p.i]
		switch c {
		case quote:
			p.i++
			return b.String(), nil
		case '\\':
			p.i++
			if p.i >= len(p.s) {
				return "", p.errf("unterminated string")
			}
			e := p.s[p.i]
			p.i++
			switch e {
			case 'a':
				b.WriteByte(7)
			case 'b':
				b.WriteByte(8)
			case 'f':
				b.WriteByte(12)
			case 'n':
				b.WriteByte('\n')
			case 'r':
				b.WriteByte('\r')
			case 't':
				b.WriteByte('\t')
			case 'v':
				b.WriteByte(11)
			case 'x':
				b.WriteByte(byte(p.digits(16, 2)))
			case 'z':
				for p.i < len(p.s) && (p.s[p.i] == ' ' || p.s[p.i] == '\t' || p.s[p.i] == '\n' || p.s[p.i] == '\r') {
					p.i++
				}
			default:
				if e >= '0' && e <= '9' {
					p.i--
					b.WriteByte(byte(p.digits(10, 3)))
				} else {
					b.WriteByte(e)
				}
			}
		default:
			b.WriteByte(c)
			p.i++
		}
	}
	return "", p.errf("unterminated string")
}

func (p *luaParser) digits(base, max int) int64 {
	start := p.i
	for p.i < len(p.s) && p.i-start < max {
		if _, err := strconv.ParseInt(p.s[p.i:p.i+1], base, 32); err != nil {
			break
		}
		p.i++
	}
	n, _ := strconv.ParseInt(p.s[start:p.i], base, 64)
	return n
}

func (p *luaParser) number() (float64, error) {
	start := p.i
	if p.i < len(p.s) && (p.s[p.i] == '-' || p.s[p.i] == '+') {
		p.i++
	}
	for p.i < len(p.s) && (isIdent(p.s[p.i]) || p.s[p.i] == '.' ||
		(p.s[p.i] == '-' || p.s[p.i] == '+') && p.i > start && strings.ContainsRune("eEpP", rune(p.s[p.i-1]))) {
		p.i++
	}
	tok := p.s[start:p.i]
	low := strings.ToLower(strings.TrimPrefix(strings.TrimPrefix(tok, "-"), "+"))
	if strings.HasPrefix(low, "0x") && !strings.ContainsAny(low, "pP") {
		n, err := strconv.ParseUint(low[2:], 16, 64)
		if err != nil {
			return 0, p.errf("bad hex number %s", tok)
		}
		if strings.HasPrefix(tok, "-") {
			return -float64(n), nil
		}
		return float64(n), nil
	}
	f, err := strconv.ParseFloat(tok, 64)
	if err != nil {
		return 0, p.errf("bad number %s", tok)
	}
	return f, nil
}

// table returns []any for a pure array, map[string]any otherwise. A table with
// both kinds of entry keeps the array items under their 1-based index.
func (p *luaParser) table() (any, error) {
	p.i++
	var arr []any
	hash := map[string]any{}
	for {
		p.skip()
		if p.i >= len(p.s) {
			return nil, p.errf("unterminated table")
		}
		if p.take('}') {
			break
		}
		key, keyed, err := p.entryKey()
		if err != nil {
			return nil, err
		}
		v, err := p.value()
		if err != nil {
			return nil, err
		}
		switch n, numeric := strconv.Atoi(key); {
		case !keyed:
			arr = append(arr, v)
		case numeric == nil && n == len(arr)+1:
			arr = append(arr, v)
		default:
			hash[key] = v
		}
		p.skip()
		if !p.take(',') && !p.take(';') {
			p.skip()
			if !p.take('}') {
				return nil, p.errf("expected , or } in a table")
			}
			break
		}
	}
	if len(hash) == 0 {
		if arr == nil {
			arr = []any{}
		}
		return arr, nil
	}
	for i, v := range arr {
		hash[strconv.Itoa(i+1)] = v
	}
	return hash, nil
}

// entryKey reads ["k"] = , [1] = or name = , and reports whether the entry was
// keyed at all. A positional entry leaves the position untouched.
func (p *luaParser) entryKey() (string, bool, error) {
	if p.s[p.i] == '[' {
		save := p.i
		if _, long := p.longBracket(); long {
			p.i = save
			return "", false, nil
		}
		p.i++
		k, err := p.value()
		if err != nil {
			return "", false, err
		}
		p.skip()
		if !p.take(']') {
			return "", false, p.errf("expected ] after a table key")
		}
		p.skip()
		if !p.take('=') {
			return "", false, p.errf("expected = after a table key")
		}
		return keyString(k), true, nil
	}
	save := p.i
	id := p.ident()
	if id == "" {
		return "", false, nil
	}
	p.skip()
	if p.i < len(p.s) && p.s[p.i] == '=' && (p.i+1 >= len(p.s) || p.s[p.i+1] != '=') {
		p.i++
		return id, true, nil
	}
	p.i = save
	return "", false, nil
}

func keyString(k any) string {
	switch v := k.(type) {
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(v)
	}
	return ""
}

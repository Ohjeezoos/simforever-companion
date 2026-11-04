package companion

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

// SavedVariablesFile is the file the addon's .toc makes the client write.
const SavedVariablesFile = "SimforeverProbe.lua"

// CombatLogFile supports older fixed filenames alongside timestamped session logs.
const CombatLogFile = "WoWCombatLog.txt"

// CombatLogGlob matches a session's log whatever it is called.
const CombatLogGlob = "WoWCombatLog*.txt"

// InstallRoots returns the WoW install folders worth searching: the explicit
// path first, then the standard Windows locations, the Battle.net install
// registry keys if reg.exe can be reached, and the Windows drives mounted
// under /mnt on WSL. Only folders that exist are returned.
func InstallRoots(explicit string) []string {
	var cand []string
	if explicit != "" {
		cand = append(cand, explicit)
	}
	cand = append(cand, standardRoots()...)
	cand = append(cand, registryRoots()...)
	seen := map[string]bool{}
	out := []string{}
	for _, c := range cand {
		c = filepath.Clean(c)
		if c == "" || c == "." || seen[c] {
			continue
		}
		seen[c] = true
		if _, err := os.Stat(c); err == nil {
			out = append(out, c)
		}
	}
	return out
}

func standardRoots() []string {
	var out []string
	add := func(base string) {
		out = append(out,
			filepath.Join(base, "Program Files (x86)", "World of Warcraft"),
			filepath.Join(base, "Program Files", "World of Warcraft"),
			filepath.Join(base, "Games", "World of Warcraft"),
			filepath.Join(base, "World of Warcraft"),
		)
	}
	if runtime.GOOS == "windows" {
		for _, env := range []string{"ProgramFiles(x86)", "ProgramFiles", "ProgramW6432"} {
			if v := os.Getenv(env); v != "" {
				out = append(out, filepath.Join(v, "World of Warcraft"))
			}
		}
		for _, drive := range []string{"C:", "D:", "E:", "F:"} {
			add(drive + `\`)
		}
		return out
	}
	for _, drive := range mountedDrives() {
		add(drive)
	}
	if home, err := os.UserHomeDir(); err == nil {
		for _, wine := range []string{".wine", "Games/world-of-warcraft", ".local/share/lutris/wine/wow"} {
			add(filepath.Join(home, wine, "drive_c"))
		}
	}
	return out
}

// mountedDrives lists /mnt/c style mounts, which is how WSL sees the Windows
// disks the game is actually installed on.
func mountedDrives() []string {
	entries, err := os.ReadDir("/mnt")
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() && len(e.Name()) == 1 && e.Name()[0] >= 'a' && e.Name()[0] <= 'z' {
			out = append(out, filepath.Join("/mnt", e.Name()))
		}
	}
	return out
}

// registryKeys are where a Battle.net install records the game folder.
var registryKeys = []struct{ key, value string }{
	{`HKLM\SOFTWARE\WOW6432Node\Blizzard Entertainment\World of Warcraft`, "InstallPath"},
	{`HKLM\SOFTWARE\Blizzard Entertainment\World of Warcraft`, "InstallPath"},
	{`HKCU\SOFTWARE\Blizzard Entertainment\World of Warcraft`, "InstallPath"},
	{`HKLM\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\World of Warcraft`, "InstallLocation"},
}

// registryRoots asks the Windows registry where the game is. On WSL reg.exe is
// reachable through the mounted system drive, so this works there too. Any
// failure just means one fewer candidate.
func registryRoots() []string {
	exe := regExe()
	if exe == "" {
		return nil
	}
	var out []string
	for _, k := range registryKeys {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		b, err := exec.CommandContext(ctx, exe, "query", k.key, "/v", k.value).Output()
		cancel()
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(b), "\n") {
			if !strings.Contains(line, "REG_") {
				continue
			}
			fields := strings.SplitN(strings.TrimSpace(line), "REG_", 2)
			if len(fields) != 2 {
				continue
			}
			v := strings.TrimSpace(fields[1])
			if i := strings.IndexByte(v, ' '); i >= 0 {
				v = strings.TrimSpace(v[i:])
			}
			if v = strings.TrimRight(strings.TrimSpace(v), `\`); v != "" {
				out = append(out, winPath(v))
			}
		}
	}
	return out
}

func regExe() string {
	if runtime.GOOS == "windows" {
		return "reg"
	}
	for _, p := range []string{"/mnt/c/Windows/System32/reg.exe", "/mnt/c/Windows/system32/reg.exe"} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	p, err := exec.LookPath("reg.exe")
	if err != nil {
		return ""
	}
	return p
}

// winPath turns C:\Games\WoW into /mnt/c/Games/WoW when we are not on Windows.
func winPath(p string) string {
	if runtime.GOOS == "windows" {
		return p
	}
	if len(p) > 2 && p[1] == ':' && (p[2] == '\\' || p[2] == '/') {
		return filepath.Join("/mnt", strings.ToLower(p[:1]), filepath.FromSlash(strings.ReplaceAll(p[3:], `\`, "/")))
	}
	return p
}

// CombatLogPaths returns every WoWCombatLog.txt under the given roots, one per
// game flavour, sorted and deduplicated.
func CombatLogPaths(roots []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, root := range roots {
		for _, p := range combatLogsIn(root) {
			if !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	sort.Strings(out)
	return out
}

func combatLogsIn(root string) []string {
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return nil
	}
	var out []string
	for _, flavour := range append([]string{root}, flavourDirs(root)...) {
		logs, ok := child(flavour, "Logs")
		if !ok {
			continue
		}
		matches, err := filepath.Glob(filepath.Join(logs, CombatLogGlob))
		if err != nil {
			continue
		}
		for _, f := range matches {
			if st, err := os.Stat(f); err == nil && st.Mode().IsRegular() {
				out = append(out, f)
			}
		}
	}
	return out
}

// ProbePaths returns every account-wide SimforeverProbe.lua under the given
// roots, sorted and deduplicated.
func ProbePaths(roots []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, root := range roots {
		for _, p := range probePathsIn(root) {
			if !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	sort.Strings(out)
	return out
}

// probePathsIn walks one install root: every flavour folder under it (and the
// root itself, in case the root already is one), then
// WTF/Account/<account>/SavedVariables/SimforeverProbe.lua.
func probePathsIn(root string) []string {
	info, err := os.Stat(root)
	if err != nil {
		return nil
	}
	if !info.IsDir() {
		// An explicit path may point straight at the saved variables file.
		return []string{filepath.Clean(root)}
	}
	var out []string
	for _, flavour := range append([]string{root}, flavourDirs(root)...) {
		wtf, ok := child(flavour, "WTF")
		if !ok {
			continue
		}
		accounts, ok := child(wtf, "Account")
		if !ok {
			continue
		}
		entries, err := os.ReadDir(accounts)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			sv, ok := child(filepath.Join(accounts, e.Name()), "SavedVariables")
			if !ok {
				continue
			}
			f, ok := child(sv, SavedVariablesFile)
			if !ok {
				continue
			}
			if st, err := os.Stat(f); err == nil && st.Mode().IsRegular() {
				out = append(out, f)
			}
		}
	}
	return out
}

// flavourDirs are the _classic_beta_, _classic_era_, _retail_ style folders.
// Matching the underscore pattern picks up PTR and beta folders too.
func flavourDirs(root string) []string {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() && len(n) > 2 && strings.HasPrefix(n, "_") && strings.HasSuffix(n, "_") {
			out = append(out, filepath.Join(root, n))
		}
	}
	return out
}

// child finds one entry ignoring case, because the game's folder names are
// case insensitive on Windows but the paths may be read on a case sensitive
// filesystem.
func child(dir, name string) (string, bool) {
	p := filepath.Join(dir, name)
	if _, err := os.Stat(p); err == nil {
		return p, true
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", false
	}
	for _, e := range entries {
		if strings.EqualFold(e.Name(), name) {
			return filepath.Join(dir, e.Name()), true
		}
	}
	return "", false
}

// Discover is the whole search: roots, then the files under them.
func Discover(explicit string) (roots, paths, logs []string) {
	roots = InstallRoots(explicit)
	return roots, ProbePaths(roots), CombatLogPaths(roots)
}

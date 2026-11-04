package companion

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// fakeInstall builds a directory tree shaped like a real WoW folder.
func fakeInstall(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := []string{
		// the three flavours the task names, one of them with lowercase WTF
		filepath.Join("_classic_beta_", "WTF", "Account", "ALPHA", "SavedVariables", SavedVariablesFile),
		filepath.Join("_classic_beta_", "WTF", "Account", "ALPHA", "SavedVariables", "SomethingElse.lua"),
		filepath.Join("_classic_era_", "wtf", "Account", "BETA#1", "SavedVariables", SavedVariablesFile),
		filepath.Join("_retail_", "WTF", "Account", "GAMMA", "SavedVariables", "Details.lua"),
		// a per-character file, which the addon never writes
		filepath.Join("_retail_", "WTF", "Account", "GAMMA", "Realm", "Char", "SavedVariables", SavedVariablesFile),
		// a PTR folder, which the underscore rule should also pick up
		filepath.Join("_classic_era_ptr_", "WTF", "Account", "ALPHA", "SavedVariables", SavedVariablesFile),
		filepath.Join("Data", "indices", "junk.lua"),
	}
	for _, f := range files {
		p := filepath.Join(root, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("-- x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestProbePathsFindsEveryFlavour(t *testing.T) {
	root := fakeInstall(t)
	got := ProbePaths([]string{root})
	want := []string{
		filepath.Join(root, "_classic_beta_", "WTF", "Account", "ALPHA", "SavedVariables", SavedVariablesFile),
		filepath.Join(root, "_classic_era_", "wtf", "Account", "BETA#1", "SavedVariables", SavedVariablesFile),
		filepath.Join(root, "_classic_era_ptr_", "WTF", "Account", "ALPHA", "SavedVariables", SavedVariablesFile),
	}
	if len(got) != len(want) {
		t.Fatalf("found %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("path %d = %s, want %s", i, got[i], want[i])
		}
	}
}

func TestProbePathsAcceptsFlavourFolderDirectly(t *testing.T) {
	root := filepath.Join(fakeInstall(t), "_classic_beta_")
	got := ProbePaths([]string{root})
	if len(got) != 1 || filepath.Base(got[0]) != SavedVariablesFile {
		t.Fatalf("got %v", got)
	}
}

func TestProbePathsAcceptsAFileDirectly(t *testing.T) {
	p := filepath.Join(fakeInstall(t), "_classic_beta_", "WTF", "Account", "ALPHA", "SavedVariables", SavedVariablesFile)
	got := ProbePaths([]string{p})
	if len(got) != 1 || got[0] != p {
		t.Fatalf("got %v, want [%s]", got, p)
	}
}

func TestProbePathsIgnoresUnrelatedTrees(t *testing.T) {
	if got := ProbePaths([]string{t.TempDir()}); len(got) != 0 {
		t.Fatalf("got %v, want nothing", got)
	}
	if got := ProbePaths([]string{filepath.Join(t.TempDir(), "missing")}); len(got) != 0 {
		t.Fatalf("got %v, want nothing", got)
	}
}

func TestInstallRootsPutsTheExplicitPathFirstAndDropsMissingOnes(t *testing.T) {
	root := fakeInstall(t)
	got := InstallRoots(root)
	if len(got) == 0 || got[0] != root {
		t.Fatalf("got %v, want %s first", got, root)
	}
	for _, r := range got {
		if _, err := os.Stat(r); err != nil {
			t.Errorf("returned a root that does not exist: %s", r)
		}
	}
	if got := InstallRoots(filepath.Join(root, "nope")); len(got) > 0 && got[0] == filepath.Join(root, "nope") {
		t.Errorf("a missing explicit path should not be returned")
	}
}

func TestWinPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("winPath only rewrites when the paths are read from outside Windows")
	}
	for in, want := range map[string]string{
		`C:\Program Files (x86)\World of Warcraft`: "/mnt/c/Program Files (x86)/World of Warcraft",
		`D:\Games\WoW`:  "/mnt/d/Games/WoW",
		"/already/unix": "/already/unix",
	} {
		if got := winPath(in); got != want {
			t.Errorf("winPath(%q) = %q, want %q", in, got, want)
		}
	}
}

package companion

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Ohjeezoos/simforever-companion/platform/probe"
)

func TestStateDedupePersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s, err := LoadState(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.Seen("emberlash-forever", "abc") {
		t.Fatal("empty state cannot have seen anything")
	}
	s.Record("emberlash-forever", Entry{Character: "Emberlash", Realm: "Forever",
		Path: "/w/SimforeverProbe.lua", Hash: "abc", UploadedAt: time.Unix(1700000000, 0).UTC()})
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}

	back, err := LoadState(path)
	if err != nil {
		t.Fatal(err)
	}
	if !back.Seen("emberlash-forever", "abc") {
		t.Fatal("restarting must not re-upload the same payload")
	}
	if back.Seen("emberlash-forever", "def") {
		t.Fatal("new data must not be treated as already uploaded")
	}
	if back.Seen("other-realm", "abc") {
		t.Fatal("the hash of one character must not suppress another")
	}
	if back.Entries["emberlash-forever"].UploadedAt.Unix() != 1700000000 {
		t.Fatalf("upload time lost: %+v", back.Entries)
	}
}

func TestStateSurvivesCorruption(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte("{half writ"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := LoadState(path)
	if err != nil {
		t.Fatalf("a corrupt state file must not stop the program: %v", err)
	}
	if len(s.Entries) != 0 {
		t.Fatalf("got %v", s.Entries)
	}
}

func TestEntryKey(t *testing.T) {
	exp := &probe.Export{}
	exp.Character.Name, exp.Character.Realm = "Emberlash", "Forever"
	if got := EntryKey(exp, "/w/x.lua"); got != "emberlash-forever" {
		t.Fatalf("got %q", got)
	}
	if got := EntryKey(&probe.Export{}, "/w/x.lua"); got != "/w/x.lua" {
		t.Fatalf("a nameless export must fall back to the path, got %q", got)
	}
}

func TestHashChangesWithContent(t *testing.T) {
	if Hash([]byte("a")) == Hash([]byte("b")) {
		t.Fatal("different payloads must hash differently")
	}
	if Hash([]byte("a")) != Hash([]byte("a")) {
		t.Fatal("the same payload must hash the same")
	}
}

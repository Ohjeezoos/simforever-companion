package companion

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"strings"
	"time"

	"github.com/Ohjeezoos/simforever-companion/platform/probe"
)

// Entry is what was last uploaded for one character.
type Entry struct {
	Character  string    `json:"character"`
	Realm      string    `json:"realm"`
	Path       string    `json:"path"`
	Hash       string    `json:"hash"`
	UploadedAt time.Time `json:"uploadedAt"`
}

// State is the dedupe record, so restarting the companion does not re-upload
// what the platform already has.
type State struct {
	path    string
	Entries map[string]Entry `json:"entries"`
}

func LoadState(path string) (*State, error) {
	s := &State{path: path, Entries: map[string]Entry{}}
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, s); err != nil {
		// A corrupt state file costs one duplicate upload, which the platform
		// tolerates; losing the ability to run does not.
		return &State{path: path, Entries: map[string]Entry{}}, nil
	}
	if s.Entries == nil {
		s.Entries = map[string]Entry{}
	}
	s.path = path
	return s, nil
}

// Seen reports whether this exact payload was already uploaded for this key.
func (s *State) Seen(key, hash string) bool {
	e, ok := s.Entries[key]
	return ok && e.Hash == hash
}

func (s *State) Record(key string, e Entry) { s.Entries[key] = e }

func (s *State) Save() error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(s.path, append(b, '\n'))
}

// EntryKey identifies the character the export belongs to, falling back to the
// file path when the addon recorded no name.
func EntryKey(exp *probe.Export, path string) string {
	name := strings.TrimSpace(exp.Character.Name)
	if name == "" {
		return path
	}
	realm := strings.TrimSpace(exp.Character.Realm)
	return strings.ToLower(name + "-" + realm)
}

// Hash identifies the payload. It is taken over the canonical JSON that is
// actually sent, so a file the game rewrote without new data is not re-sent.
func Hash(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

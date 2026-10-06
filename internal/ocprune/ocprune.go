// Package ocprune keeps the per-session drop list for a live OpenCode prune.
//
// ctxed sorts what the model still sees into categories and records the
// categories the user picks; the OpenCode plugin reads the same file before
// each request and removes the listed messages. The file is the only thing the
// two share:
//
//	$CTXED_STATE_DIR/<session>.json   (default ~/.local/state/ctxed/opencode)
//
// The drop list only grows. Each prune sorts only what is still sent: entries
// after the session's last compaction, minus everything already dropped.
package ocprune

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/simranjeetc/ctxed/internal/categorize"
	"github.com/simranjeetc/ctxed/internal/harness"
	"github.com/simranjeetc/ctxed/internal/session"
)

// Category is one entry of the last listing, numbered as the user saw it.
type Category struct {
	ID          int      `json:"id"`
	Label       string   `json:"label"`
	EntryIDs    []string `json:"entryIds"`
	ToolCallIDs []string `json:"toolCallIds"`
	Tokens      int      `json:"tokens"`
}

// Dropped is what the plugin removes from every request.
type Dropped struct {
	IDs         []string `json:"ids"`
	ToolCallIDs []string `json:"toolCallIds"`
}

// State is one session's file.
type State struct {
	Session   string     `json:"session"`
	Dropped   Dropped    `json:"dropped"`
	Listing   []Category `json:"listing"`
	UpdatedAt string     `json:"updatedAt,omitempty"`
}

// ErrBadSession reports a session id that cannot safely name a file.
var ErrBadSession = harness.ErrBadOpenCodeSession

// ValidSession checks a session id before it becomes part of a path.
func ValidSession(id string) error { return harness.ValidOpenCodeSession(id) }

// Dir is the state directory: $CTXED_STATE_DIR, or
// ~/.local/state/ctxed/opencode. The plugin resolves the same default from the
// home directory, so a server and a shell with different environments agree.
func Dir() (string, error) {
	if d := os.Getenv("CTXED_STATE_DIR"); d != "" {
		return d, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot find the home directory for the state file: %w", err)
	}
	return filepath.Join(home, ".local", "state", "ctxed", "opencode"), nil
}

// Path is the state file for a session.
func Path(dir, sessionID string) string { return filepath.Join(dir, sessionID+".json") }

// Load reads a session's state; a missing file is an empty state.
func Load(dir, sessionID string) (State, error) {
	if err := ValidSession(sessionID); err != nil {
		return State{}, err
	}
	data, err := os.ReadFile(Path(dir, sessionID))
	if errors.Is(err, os.ErrNotExist) {
		return State{Session: sessionID}, nil
	}
	if err != nil {
		return State{}, err
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return State{}, fmt.Errorf("state file %s: %w", Path(dir, sessionID), err)
	}
	s.Session = sessionID
	return s, nil
}

// Save writes the state atomically: the plugin may read it at any moment.
func Save(dir string, s State) error {
	if err := ValidSession(s.Session); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	s.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if s.Dropped.IDs == nil {
		s.Dropped.IDs = []string{}
	}
	if s.Dropped.ToolCallIDs == nil {
		s.Dropped.ToolCallIDs = []string{}
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, s.Session+".*.tmp")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), Path(dir, s.Session))
}

// LiveView reduces an OpenCode export to what can still be pruned: entries
// after the last compaction item, minus entries already on the drop list. It
// returns how many entries it left out. The compaction item itself is never an
// entry, so the summary is never offered for dropping.
func LiveView(doc *session.Document, dropped Dropped) int {
	last := -1
	for i, it := range doc.Items {
		var p struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(it.Raw, &p) == nil && p.Type == "compaction" {
			last = i
		}
	}
	ids := toSet(dropped.IDs)
	calls := toSet(dropped.ToolCallIDs)
	var out []int
	for i, it := range doc.Items {
		if it.EntryIndex < 0 {
			continue
		}
		e := entryAt(doc, it.EntryIndex)
		if e == nil {
			continue
		}
		if i < last || e.Kind == session.KindSummary || ids[e.ID] || anyIn(e.CallIDs, calls) || anyIn(e.ResultIDs, calls) {
			out = append(out, e.Index)
		}
	}
	doc.Drop(out)
	return len(out)
}

// NewListing turns a categorization of the live view into the numbered listing
// the user picks from, carrying each category's tool-call ids so a dropped tool
// result goes with its call.
func NewListing(f categorize.File, doc *session.Document) []Category {
	byID := make(map[string]*session.Entry, len(doc.Entries))
	for _, e := range doc.Entries {
		byID[e.ID] = e
	}
	out := make([]Category, 0, len(f.Categories))
	for _, c := range f.Categories {
		calls := map[string]bool{}
		for _, id := range c.IDs {
			if e := byID[id]; e != nil {
				for _, x := range append(append([]string{}, e.CallIDs...), e.ResultIDs...) {
					calls[x] = true
				}
			}
		}
		out = append(out, Category{
			ID:          c.ID,
			Label:       c.Label,
			EntryIDs:    append([]string{}, c.IDs...),
			ToolCallIDs: sortedKeys(calls),
			Tokens:      c.Tokens,
		})
	}
	return out
}

// Drop adds the chosen categories of the last listing to the drop list and
// returns them. An unknown number changes nothing.
func (s *State) Drop(numbers []int) ([]Category, error) {
	if len(s.Listing) == 0 {
		return nil, errors.New("no categories listed yet; run `ctxed opencode categorize` first")
	}
	byID := map[int]Category{}
	for _, c := range s.Listing {
		byID[c.ID] = c
	}
	var chosen []Category
	seen := map[int]bool{}
	for _, n := range numbers {
		c, ok := byID[n]
		if !ok {
			return nil, fmt.Errorf("no category %d in the last listing (it has 1-%d)", n, len(s.Listing))
		}
		if !seen[n] {
			seen[n] = true
			chosen = append(chosen, c)
		}
	}
	ids, calls := toSet(s.Dropped.IDs), toSet(s.Dropped.ToolCallIDs)
	for _, c := range chosen {
		for _, id := range c.EntryIDs {
			if !ids[id] {
				ids[id] = true
				s.Dropped.IDs = append(s.Dropped.IDs, id)
			}
		}
		for _, id := range c.ToolCallIDs {
			if !calls[id] {
				calls[id] = true
				s.Dropped.ToolCallIDs = append(s.Dropped.ToolCallIDs, id)
			}
		}
	}
	return chosen, nil
}

func entryAt(doc *session.Document, index int) *session.Entry {
	for _, e := range doc.Entries {
		if e.Index == index {
			return e
		}
	}
	return nil
}

func toSet(xs []string) map[string]bool {
	m := make(map[string]bool, len(xs))
	for _, x := range xs {
		m[x] = true
	}
	return m
}

func anyIn(xs []string, set map[string]bool) bool {
	for _, x := range xs {
		if set[x] {
			return true
		}
	}
	return false
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

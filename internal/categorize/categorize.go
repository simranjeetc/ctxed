// Package categorize groups a session's entries into a small set of labeled,
// high-level categories, published as an editable file.
package categorize

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/simranjeetc/ctxed/internal/session"
	"github.com/simranjeetc/ctxed/internal/tokenize"
)

// DefaultMaxCategories bounds how many categories a session may be split into.
const DefaultMaxCategories = 5

// Category is one high-level group of entries.
type Category struct {
	ID     int      `json:"id"`
	Label  string   `json:"label"`
	IDs    []string `json:"entryIds"`
	Count  int      `json:"entryCount"`
	Tokens int      `json:"tokens"`
}

// TokenInfo describes how tokens were counted.
type TokenInfo struct {
	Name        string `json:"name"`
	Approximate bool   `json:"approximate"`
}

// File is the editable categorization written by categorize and read by prune.
type File struct {
	Source        string     `json:"source"`
	Session       string     `json:"session"`
	Tokenizer     TokenInfo  `json:"tokenizer"`
	Categories    []Category `json:"categories"`
	Uncategorized []string   `json:"uncategorized,omitempty"`
}

// Prompt builds the categorization prompt for a session. The prompt is bounded
// by maxBytes, because a prompt proportional to a long session is slow (tens of
// seconds) and the model stops following the instruction. Two bounds apply: at
// most maxEntries entries are listed, sampled evenly so the whole session is
// represented, and the per-entry snippet is shortened to fit the remaining
// budget. Entries left out are reported as uncategorized by Parse; naming the
// session's topics needs a representative view, not every entry.
func Prompt(doc *session.Document, max int, maxBytes int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You are given the entries of a working session, oldest first.\n")
	fmt.Fprintf(&b, "Group them into between 2 and %d high-level categories that describe what the session is about\n", max)
	fmt.Fprintf(&b, "(for example \"Claude Code adapter work\", \"OpenCode investigation\").\n")
	fmt.Fprintf(&b, "Use each entry id exactly as given. Assign every entry to exactly one category.\n")
	fmt.Fprintf(&b, "Return only JSON, no prose:\n")
	fmt.Fprintf(&b, `{"categories":[{"label":"...","ids":["...","..."]}]}`+"\n\nEntries:\n")
	header := b.String()

	if len(doc.Entries) == 0 {
		return header
	}

	const (
		maxEntries = 250
		maxSnippet = 300
		minSnippet = 24
	)

	entries := doc.Entries
	if len(entries) > maxEntries {
		entries = sampleEntries(entries, maxEntries)
	}

	budget := maxBytes - len(header)
	if budget < 0 {
		budget = 0
	}

	// The largest snippet length at which the chosen entries fit the budget.
	snippetLen := minSnippet
	lo, hi := minSnippet, maxSnippet
	for lo <= hi {
		mid := (lo + hi) / 2
		if promptFits(entries, budget, mid) {
			snippetLen = mid
			lo = mid + 1
		} else {
			hi = mid - 1
		}
	}

	for _, e := range entries {
		fmt.Fprintf(&b, "- id=%s role=%s kind=%s: %s\n", e.ID, e.Role, e.Kind, snippet(e.Text, snippetLen))
	}
	return b.String()
}

// entryLineLen is the rendered size of one entry line at a snippet length. The
// estimate is deliberately conservative: it assumes the snippet is truncated and
// carries the "…" marker, so the fitted prompt stays within budget.
func entryLineLen(e *session.Entry, snippetLen int) int {
	// "- id=" (5) + id + " role=" (6) + role + " kind=" (6) + kind + ": " (2)
	// + snippet + "…" (3) + "\n" (1)
	const fixed = 23
	return len(e.ID) + len(e.Role) + len(e.Kind) + snippetLen + fixed
}

func promptFits(entries []*session.Entry, budget, snippetLen int) bool {
	total := 0
	for _, e := range entries {
		total += entryLineLen(e, snippetLen)
		if total > budget {
			return false
		}
	}
	return true
}

// sampleEntries picks up to n entries spread evenly across the slice, so a
// sampled view represents the whole session rather than only its head.
func sampleEntries(entries []*session.Entry, n int) []*session.Entry {
	if n >= len(entries) {
		return entries
	}
	out := make([]*session.Entry, 0, n)
	for i := 0; i < n; i++ {
		idx := i * (len(entries) - 1) / (n - 1)
		out = append(out, entries[idx])
	}
	return out
}

type modelResponse struct {
	Categories []struct {
		Label string   `json:"label"`
		IDs   []string `json:"ids"`
	} `json:"categories"`
}

// Parse turns a model response into a validated File, resolving ids against the
// session and enforcing the category bounds.
func Parse(text string, doc *session.Document, tok tokenize.Tokenizer, max int) (File, error) {
	if max < 2 {
		max = 2
	}
	raw, err := extractJSON(text)
	if err != nil {
		return File{}, err
	}
	var mr modelResponse
	if err := json.Unmarshal([]byte(raw), &mr); err != nil {
		return File{}, fmt.Errorf("model response is not categorization JSON: %w", err)
	}
	if len(mr.Categories) < 2 {
		return File{}, fmt.Errorf("model returned %d categories; at least 2 are required", len(mr.Categories))
	}
	if len(mr.Categories) > max {
		return File{}, fmt.Errorf("model returned %d categories; the maximum is %d", len(mr.Categories), max)
	}

	known := make(map[string]*session.Entry, len(doc.Entries))
	order := make([]string, 0, len(doc.Entries))
	for _, e := range doc.Entries {
		known[e.ID] = e
		order = append(order, e.ID)
	}

	assigned := map[string]bool{}
	f := File{Source: doc.Source, Tokenizer: TokenInfo{Name: tok.Name(), Approximate: tok.Approximate()}}
	for i, c := range mr.Categories {
		label := strings.TrimSpace(c.Label)
		if label == "" {
			label = fmt.Sprintf("Category %d", i+1)
		}
		cat := Category{Label: label}
		seen := map[string]bool{}
		for _, id := range c.IDs {
			e, ok := known[id]
			if !ok || seen[id] || assigned[id] {
				continue
			}
			seen[id] = true
			assigned[id] = true
			cat.IDs = append(cat.IDs, id)
			cat.Count++
			cat.Tokens += tok.Count(e.Text)
		}
		if cat.Count > 0 {
			f.Categories = append(f.Categories, cat)
		}
	}
	if len(f.Categories) < 2 {
		return File{}, fmt.Errorf("after resolving ids, fewer than 2 categories have entries")
	}
	for i := range f.Categories {
		f.Categories[i].ID = i + 1
	}
	for _, id := range order {
		if !assigned[id] {
			f.Uncategorized = append(f.Uncategorized, id)
		}
	}
	return f, nil
}

// Load parses and validates a categories document against the session.
func Load(data []byte, doc *session.Document) (File, error) {
	var f File
	if err := json.Unmarshal(data, &f); err != nil {
		return File{}, fmt.Errorf("categories file: %w", err)
	}
	if err := Validate(f, doc); err != nil {
		return File{}, err
	}
	return f, nil
}

// ReadFile loads an (edited) categories file and validates it against the session.
func ReadFile(path string, doc *session.Document) (File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return File{}, err
	}
	return Load(data, doc)
}

// Marshal renders the categories file as JSON, the same bytes WriteFile writes
// (minus the trailing newline), so a caller can print it to stdout.
func Marshal(f File) ([]byte, error) {
	return json.MarshalIndent(f, "", "  ")
}

// WriteFile writes the categories file.
func WriteFile(path string, f File) error {
	data, err := Marshal(f)
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// Validate rejects ids not present in the session and double assignment.
func Validate(f File, doc *session.Document) error {
	known := make(map[string]bool, len(doc.Entries))
	for _, e := range doc.Entries {
		known[e.ID] = true
	}
	seen := map[string]bool{}
	check := func(ids []string) error {
		for _, id := range ids {
			if !known[id] {
				return fmt.Errorf("categories file references unknown entry id %q", id)
			}
			if seen[id] {
				return fmt.Errorf("entry id %q is assigned more than once", id)
			}
			seen[id] = true
		}
		return nil
	}
	for _, c := range f.Categories {
		if err := check(c.IDs); err != nil {
			return err
		}
	}
	return check(f.Uncategorized)
}

// Select returns the entry ids belonging to the given category ids.
func Select(f File, cats []int) ([]string, error) {
	want := map[int]bool{}
	for _, c := range cats {
		want[c] = true
	}
	var out []string
	found := map[int]bool{}
	for _, c := range f.Categories {
		if want[c.ID] {
			found[c.ID] = true
			out = append(out, c.IDs...)
		}
	}
	for _, c := range cats {
		if !found[c] {
			return nil, fmt.Errorf("no category with id %d", c)
		}
	}
	return out, nil
}

// Render writes the high-level table.
func Render(w io.Writer, f File) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "CAT\tLABEL\tENTRIES\tTOKENS")
	for _, c := range f.Categories {
		fmt.Fprintf(tw, "%d\t%s\t%d\t%d\n", c.ID, c.Label, c.Count, c.Tokens)
	}
	if len(f.Uncategorized) > 0 {
		fmt.Fprintf(tw, "%s\t%s\t%d\t%d\n", "-", "(uncategorized)", len(f.Uncategorized), 0)
	}
	return tw.Flush()
}

// SortedCategoryIDs is a small helper for stable output in tests.
func SortedCategoryIDs(f File) []int {
	ids := make([]int, 0, len(f.Categories))
	for _, c := range f.Categories {
		ids = append(ids, c.ID)
	}
	sort.Ints(ids)
	return ids
}

func extractJSON(text string) (string, error) {
	start := strings.IndexByte(text, '{')
	end := strings.LastIndexByte(text, '}')
	if start < 0 || end <= start {
		return "", fmt.Errorf("model response contained no JSON object")
	}
	return text[start : end+1], nil
}

func snippet(s string, max int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > max {
		s = s[:max] + "…"
	}
	return s
}

// Package categorize groups a session's entries into a small set of labeled,
// high-level categories, published as an editable file.
package categorize

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strconv"
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
	// Status is the model's judgement of the topic: StatusDone, StatusInProgress,
	// or "" when it gave none (or an unknown value).
	Status string `json:"status,omitempty"`
}

// Topic statuses a model may return.
const (
	StatusDone       = "done"
	StatusInProgress = "in_progress"
)

// maxPending bounds the pending items kept from a model response.
const maxPending = 5

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
	// Pending lists what the session has left open, as the model read it.
	Pending []string `json:"pending,omitempty"`
}

// Prompt builds the categorization prompt for a session. The prompt is bounded
// by maxBytes, because a prompt proportional to a long session is slow (tens of
// seconds) and the model stops following the instruction. Two bounds apply: at
// most maxEntries entries are listed, sampled evenly so the whole session is
// represented, and the per-entry snippet is shortened to fit the remaining
// budget. Entries left out are reported as uncategorized by Parse; naming the
// session's topics needs a representative view, not every entry.
func Prompt(doc *session.Document, maxCats int, maxBytes int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You are given the entries of a working session, oldest first.\n")
	fmt.Fprintf(&b, "Group them into between 1 and %d high-level categories that describe what the session is about\n", maxCats)
	fmt.Fprintf(&b, "(for example \"Claude Code adapter work\", \"OpenCode investigation\").\n")
	fmt.Fprintf(&b, "Refer to entries by their [number]; give a run of consecutive entries as a range like \"4-17\".\n")
	fmt.Fprintf(&b, "Assign every entry to exactly one category.\n")
	fmt.Fprintf(&b, "For each category give a status: \"done\" if its work was finished, confirmed or abandoned;\n")
	fmt.Fprintf(&b, "\"in_progress\" if it is still being worked on or waiting on something. The last entries are\n")
	fmt.Fprintf(&b, "the most recent: a topic being worked on there is in_progress unless they say it is finished.\n")
	fmt.Fprintf(&b, "Also list up to %d short items that are still pending (open questions, unfinished steps,\n", maxPending)
	fmt.Fprintf(&b, "things the user asked for that are not done yet).\n")
	fmt.Fprintf(&b, "Return only JSON, no prose:\n")
	fmt.Fprintf(&b, `{"categories":[{"label":"...","status":"done","ids":["1-12","15"]}],"pending":["..."]}`+"\n\nEntries:\n")
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

	pos := make(map[*session.Entry]int, len(doc.Entries))
	for i, e := range doc.Entries {
		pos[e] = i + 1
	}
	for _, e := range entries {
		fmt.Fprintf(&b, "- [%d] role=%s kind=%s: %s\n", pos[e], e.Role, e.Kind, snippet(e.Text, snippetLen))
	}
	return b.String()
}

// entryLineLen is the rendered size of one entry line at a snippet length. The
// estimate is deliberately conservative: it assumes the snippet is truncated and
// carries the "…" marker, so the fitted prompt stays within budget.
func entryLineLen(e *session.Entry, snippetLen int) int {
	// "- [" (3) + number (at most 7) + "] role=" (7) + role + " kind=" (6) + kind
	// + ": " (2) + snippet + "…" (3) + "\n" (1)
	const fixed = 29
	return len(e.Role) + len(e.Kind) + snippetLen + fixed
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
		Label  string            `json:"label"`
		Status string            `json:"status"`
		IDs    []json.RawMessage `json:"ids"`
	} `json:"categories"`
	Pending []string `json:"pending"`
}

// Parse turns a model response into a validated File, resolving ids against the
// session and enforcing the category bounds.
func Parse(text string, doc *session.Document, tok tokenize.Tokenizer, maxCats int) (File, error) {
	if maxCats < 1 {
		maxCats = 1
	}
	raw, err := extractJSON(text)
	if err != nil {
		return File{}, err
	}
	var mr modelResponse
	if err := json.Unmarshal([]byte(raw), &mr); err != nil {
		return File{}, fmt.Errorf("model response is not categorization JSON: %w", err)
	}
	if len(mr.Categories) < 1 {
		return File{}, fmt.Errorf("model returned %d categories; at least 1 is required", len(mr.Categories))
	}
	if len(mr.Categories) > maxCats {
		return File{}, fmt.Errorf("model returned %d categories; the maximum is %d", len(mr.Categories), maxCats)
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
		cat := Category{Label: label, Status: normalizeStatus(c.Status)}
		seen := map[string]bool{}
		for _, id := range resolveRefs(c.IDs, order) {
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
	if len(f.Categories) < 1 {
		return File{}, fmt.Errorf("after resolving ids, no category has entries")
	}
	for i := range f.Categories {
		f.Categories[i].ID = i + 1
	}
	for _, id := range order {
		if !assigned[id] {
			f.Uncategorized = append(f.Uncategorized, id)
		}
	}
	for _, p := range mr.Pending {
		if p = strings.TrimSpace(p); p != "" && len(f.Pending) < maxPending {
			f.Pending = append(f.Pending, p)
		}
	}
	return f, nil
}

var (
	numberRe = regexp.MustCompile(`^\s*(\d+)\s*$`)
	rangeRe  = regexp.MustCompile(`^\s*(\d+)\s*-\s*(\d+)\s*$`)
)

// resolveRefs turns a category's entry references into entry ids. A reference
// is an entry's [number] from the prompt (a JSON number or a numeric string),
// a range "a-b" of numbers, or an entry id. Out-of-range numbers are ignored.
func resolveRefs(refs []json.RawMessage, order []string) []string {
	var out []string
	at := func(n int) {
		if n >= 1 && n <= len(order) {
			out = append(out, order[n-1])
		}
	}
	for _, raw := range refs {
		var n int
		if json.Unmarshal(raw, &n) == nil {
			at(n)
			continue
		}
		var s string
		if json.Unmarshal(raw, &s) != nil {
			continue
		}
		if m := numberRe.FindStringSubmatch(s); m != nil {
			n, _ = strconv.Atoi(m[1])
			at(n)
		} else if m := rangeRe.FindStringSubmatch(s); m != nil {
			lo, _ := strconv.Atoi(m[1])
			hi, _ := strconv.Atoi(m[2])
			if lo < 1 {
				lo = 1
			}
			if hi > len(order) {
				hi = len(order)
			}
			for k := lo; k <= hi; k++ {
				at(k)
			}
		} else {
			out = append(out, s)
		}
	}
	return out
}

// normalizeStatus maps a model's status to StatusDone, StatusInProgress or "".
func normalizeStatus(s string) string {
	switch strings.ToLower(strings.NewReplacer(" ", "_", "-", "_").Replace(strings.TrimSpace(s))) {
	case "done", "finished", "complete", "completed":
		return StatusDone
	case "in_progress", "pending", "open", "ongoing":
		return StatusInProgress
	}
	return ""
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
	return os.WriteFile(path, append(data, '\n'), 0o644) //nolint:gosec // G306: the categories file is a user-editable document, not a secret.
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
	_, _ = fmt.Fprintln(tw, "CAT\tLABEL\tENTRIES\tTOKENS")
	for _, c := range f.Categories {
		_, _ = fmt.Fprintf(tw, "%d\t%s\t%d\t%d\n", c.ID, c.Label, c.Count, c.Tokens)
	}
	if len(f.Uncategorized) > 0 {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%d\t%d\n", "-", "(uncategorized)", len(f.Uncategorized), 0)
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

func snippet(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > maxLen {
		s = s[:maxLen] + "…"
	}
	return s
}

// Package prune validates and applies entry removals.
package prune

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/simranjeetc/ctxed/internal/session"
)

// ParseIndices parses a comma-separated index list such as "3,7,9".
func ParseIndices(s string) ([]int, error) {
	if strings.TrimSpace(s) == "" {
		return nil, fmt.Errorf("no indices given")
	}
	fields := strings.Split(s, ",")
	out := make([]int, 0, len(fields))
	for _, f := range fields {
		f = strings.TrimSpace(f)
		n, err := strconv.Atoi(f)
		if err != nil {
			return nil, fmt.Errorf("invalid index %q", f)
		}
		out = append(out, n)
	}
	return out, nil
}

// Validate rejects out-of-range and duplicate indices.
func Validate(doc *session.Document, indices []int) error {
	valid := make(map[int]bool, len(doc.Entries))
	high := -1
	for _, e := range doc.Entries {
		valid[e.Index] = true
		if e.Index > high {
			high = e.Index
		}
	}
	seen := make(map[int]bool, len(indices))
	for _, i := range indices {
		if !valid[i] {
			return fmt.Errorf("index %d is not in the session (valid range 0-%d)", i, high)
		}
		if seen[i] {
			return fmt.Errorf("index %d given more than once", i)
		}
		seen[i] = true
	}
	return nil
}

// Orphans returns human-readable descriptions of kept entries whose tool
// results answer a tool call that the drop removes. An empty result means the
// edit is structurally sound.
func Orphans(doc *session.Document, drop []int) []string {
	dropped := make(map[int]bool, len(drop))
	for _, i := range drop {
		dropped[i] = true
	}
	issued := make(map[string]bool)
	for _, e := range doc.Entries {
		if dropped[e.Index] {
			continue
		}
		for _, id := range e.CallIDs {
			issued[id] = true
		}
	}
	var out []string
	for _, e := range doc.Entries {
		if dropped[e.Index] {
			continue
		}
		for _, id := range e.ResultIDs {
			if !issued[id] {
				out = append(out, fmt.Sprintf("entry %d (%s) answers tool call %s, which was removed", e.Index, e.Role, id))
			}
		}
	}
	return out
}

// IndicesForIDs maps stable entry ids to entry indices, returning any ids not
// present in the document.
func IndicesForIDs(doc *session.Document, ids []string) (indices []int, missing []string) {
	byID := make(map[string]int, len(doc.Entries))
	for _, e := range doc.Entries {
		byID[e.ID] = e.Index
	}
	for _, id := range ids {
		if idx, ok := byID[id]; ok {
			indices = append(indices, idx)
		} else {
			missing = append(missing, id)
		}
	}
	return indices, missing
}

// ResolveOrphans extends a drop set so no retained entry answers a tool call
// that is being dropped. It returns the final indices and a description of each
// adjustment, so a dispatch-time prune never emits an orphaned tool result.
func ResolveOrphans(doc *session.Document, drop []int) ([]int, []string) {
	dropSet := make(map[int]bool, len(drop))
	for _, i := range drop {
		dropSet[i] = true
	}
	var adjustments []string
	for {
		issued := map[string]bool{}
		for _, e := range doc.Entries {
			if dropSet[e.Index] {
				continue
			}
			for _, id := range e.CallIDs {
				issued[id] = true
			}
		}
		changed := false
		for _, e := range doc.Entries {
			if dropSet[e.Index] {
				continue
			}
			for _, rid := range e.ResultIDs {
				if !issued[rid] {
					dropSet[e.Index] = true
					adjustments = append(adjustments, fmt.Sprintf("entry %s (%s) answers tool call %s whose call is pruned; dropping it too", e.ID, e.Role, rid))
					changed = true
					break
				}
			}
		}
		if !changed {
			break
		}
	}
	out := make([]int, 0, len(dropSet))
	for i := range dropSet {
		out = append(out, i)
	}
	sort.Ints(out)
	return out, adjustments
}

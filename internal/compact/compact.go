// Package compact renders a Claude Code `/compact` instruction from a category
// selection. It is offline and deterministic: it reads a categories file and
// names the buckets to keep and the buckets to drop, so a person can paste the
// result after `/compact` in a live Claude Code session.
//
// It never rewrites a transcript and never calls a model; the instruction only
// steers Claude Code's own compaction, which rewrites history in-session.
package compact

import (
	"fmt"
	"strings"

	"github.com/simranjeetc/ctxed/internal/categorize"
)

// Instruction returns a single sentence that tells Claude Code's `/compact`
// which categories to keep and which to drop, naming each by its label. The
// categories in drop are the ones the user selected; every other category is
// kept. It returns an error if a drop id does not name a category.
func Instruction(f categorize.File, drop []int) (string, error) {
	// Reuse the prune selection validation so an unknown category id is
	// reported exactly as it is elsewhere.
	if _, err := categorize.Select(f, drop); err != nil {
		return "", err
	}

	dropSet := make(map[int]bool, len(drop))
	for _, id := range drop {
		dropSet[id] = true
	}

	var keep, discard []string
	for _, c := range f.Categories {
		if dropSet[c.ID] {
			discard = append(discard, label(c.Label))
		} else {
			keep = append(keep, label(c.Label))
		}
	}

	switch {
	case len(keep) == 0:
		return fmt.Sprintf("When you compact this session, drop the context about %s.", join(discard)), nil
	case len(discard) == 0:
		return fmt.Sprintf("When you compact this session, keep the context about %s.", join(keep)), nil
	default:
		return fmt.Sprintf("When you compact this session, keep the context about %s, and drop the context about %s.",
			join(keep), join(discard)), nil
	}
}

// label falls back to a stable placeholder for an empty label, so the sentence
// never ends in a dangling list.
func label(s string) string {
	if s = strings.TrimSpace(s); s == "" {
		return "an unlabeled category"
	}
	return s
}

// join renders a human list: "A", "A and B", "A, B, and C".
func join(labels []string) string {
	switch len(labels) {
	case 0:
		return ""
	case 1:
		return labels[0]
	case 2:
		return labels[0] + " and " + labels[1]
	default:
		return strings.Join(labels[:len(labels)-1], ", ") + ", and " + labels[len(labels)-1]
	}
}

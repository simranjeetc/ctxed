package compact_test

import (
	"strings"
	"testing"

	"github.com/simranjeetc/ctxed/internal/categorize"
	"github.com/simranjeetc/ctxed/internal/compact"
)

func file(cats ...categorize.Category) categorize.File {
	f := categorize.File{}
	for i, c := range cats {
		c.ID = i + 1
		f.Categories = append(f.Categories, c)
	}
	return f
}

func TestInstructionKeepsUnselectedAndDropsSelected(t *testing.T) {
	// Three buckets; select two of them (1 and 3) to drop, keep 2.
	f := file(
		categorize.Category{Label: "Auth refactor"},
		categorize.Category{Label: "Database tuning"},
		categorize.Category{Label: "Docs cleanup"},
	)
	got, err := compact.Instruction(f, []int{1, 3})
	if err != nil {
		t.Fatalf("Instruction: %v", err)
	}
	want := "When you compact this session, keep the context about Database tuning, and drop the context about Auth refactor and Docs cleanup."
	if got != want {
		t.Fatalf("instruction =\n  %q\nwant\n  %q", got, want)
	}
}

func TestInstructionNamesEveryBucketByLabel(t *testing.T) {
	// Four buckets, drop one; the three kept labels render with a serial comma.
	f := file(
		categorize.Category{Label: "One"},
		categorize.Category{Label: "Two"},
		categorize.Category{Label: "Three"},
		categorize.Category{Label: "Four"},
	)
	got, err := compact.Instruction(f, []int{1})
	if err != nil {
		t.Fatalf("Instruction: %v", err)
	}
	want := "When you compact this session, keep the context about Two, Three, and Four, and drop the context about One."
	if got != want {
		t.Fatalf("instruction = %q, want %q", got, want)
	}
}

func TestInstructionSingleDropSingleKeep(t *testing.T) {
	f := file(
		categorize.Category{Label: "Keep me"},
		categorize.Category{Label: "Drop me"},
	)
	got, err := compact.Instruction(f, []int{2})
	if err != nil {
		t.Fatalf("Instruction: %v", err)
	}
	want := "When you compact this session, keep the context about Keep me, and drop the context about Drop me."
	if got != want {
		t.Fatalf("instruction = %q, want %q", got, want)
	}
}

func TestInstructionAllDroppedOmitsKeepClause(t *testing.T) {
	f := file(
		categorize.Category{Label: "A"},
		categorize.Category{Label: "B"},
	)
	got, err := compact.Instruction(f, []int{1, 2})
	if err != nil {
		t.Fatalf("Instruction: %v", err)
	}
	want := "When you compact this session, drop the context about A and B."
	if got != want {
		t.Fatalf("instruction = %q, want %q", got, want)
	}
}

func TestInstructionUnknownCategoryIsError(t *testing.T) {
	f := file(categorize.Category{Label: "A"}, categorize.Category{Label: "B"})
	_, err := compact.Instruction(f, []int{9})
	if err == nil {
		t.Fatal("expected an error for an unknown category id")
	}
	if !strings.Contains(err.Error(), "no category with id 9") {
		t.Fatalf("error = %v", err)
	}
}

func TestInstructionMissingCategoryAmongValidOnesIsError(t *testing.T) {
	f := file(categorize.Category{Label: "A"}, categorize.Category{Label: "B"})
	if _, err := compact.Instruction(f, []int{1, 7}); err == nil {
		t.Fatal("expected an error when one selected category does not exist")
	}
}

func TestInstructionIsDeterministic(t *testing.T) {
	f := file(
		categorize.Category{Label: "A"},
		categorize.Category{Label: "B"},
		categorize.Category{Label: "C"},
	)
	a, err := compact.Instruction(f, []int{2})
	if err != nil {
		t.Fatal(err)
	}
	b, err := compact.Instruction(f, []int{2})
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatalf("not deterministic:\n%q\n%q", a, b)
	}
}

func TestInstructionEmptyLabelFallback(t *testing.T) {
	f := file(categorize.Category{Label: "Kept"}, categorize.Category{Label: "  "})
	got, err := compact.Instruction(f, []int{2})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "an unlabeled category") {
		t.Fatalf("empty label not handled: %q", got)
	}
}

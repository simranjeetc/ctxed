package tokenize_test

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/simranjeetc/ctxed/internal/tokenize"
)

func TestApproximation(t *testing.T) {
	a := tokenize.Approximation{}
	if a.Count("") != 0 {
		t.Fatal("empty text should count 0")
	}
	if !a.Approximate() {
		t.Fatal("approximation must report Approximate() == true")
	}
	if got := a.Count("abcd"); got != 1 {
		t.Fatalf("4 runes -> %d, want 1", got)
	}
	if got := a.Count("abcde"); got != 2 {
		t.Fatalf("5 runes -> %d, want 2", got)
	}
}

func TestDocumentedExample(t *testing.T) {
	// The approximation formula and this example are documented in
	// internal/tokenize and README.md; keep them in sync.
	if got := (tokenize.Approximation{}).Count("abcde"); got != 2 {
		t.Fatalf(`documented example: Count("abcde") = %d, want 2`, got)
	}
}

func TestResolveNoModelIsApproximate(t *testing.T) {
	tok, err := tokenize.Resolve("", "")
	if err != nil {
		t.Fatal(err)
	}
	if !tok.Approximate() {
		t.Fatalf("no model should be approximate, got %q", tok.Name())
	}
}

func TestResolveUnknownModelFallsBackToApproximate(t *testing.T) {
	tok, err := tokenize.Resolve("definitely-not-a-real-model-xyz", "")
	if err != nil {
		t.Fatalf("unknown model must not error: %v", err)
	}
	if !tok.Approximate() {
		t.Fatalf("unknown model should fall back to approximation, got %q", tok.Name())
	}
}

func TestResolveUnknownEncodingErrors(t *testing.T) {
	if _, err := tokenize.Resolve("", "no-such-encoding"); err == nil {
		t.Fatal("expected an error for an unknown encoding")
	}
}

// TestTiktokenKnownEncoding exercises the real tokenizer path. It is gated so
// the default test run stays offline and deterministic; set
// CTXED_TEST_TIKTOKEN=1 (optionally with TIKTOKEN_CACHE_DIR) to run it.
func TestTiktokenKnownEncoding(t *testing.T) {
	if os.Getenv("CTXED_TEST_TIKTOKEN") == "" {
		t.Skip("set CTXED_TEST_TIKTOKEN=1 to exercise the tiktoken path")
	}
	tok, err := tokenize.Resolve("", "cl100k_base")
	if err != nil {
		t.Fatalf("Resolve cl100k_base: %v", err)
	}
	if tok.Approximate() {
		t.Fatal("explicit encoding must be exact, not approximate")
	}
	if tok.Count("hello world") == 0 {
		t.Fatal("expected a positive token count")
	}
}

// benchmarkSessionText builds the text of a synthetic n-message session, so the
// benchmark counts tokens over a realistic workload without a fixture on disk.
func benchmarkSessionText(n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "message %d about context windows: %s\n", i, strings.Repeat("token ", 40))
	}
	return b.String()
}

func BenchmarkCount(b *testing.B) {
	text := benchmarkSessionText(2000)
	tok := tokenize.Approximation{}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = tok.Count(text)
	}
}

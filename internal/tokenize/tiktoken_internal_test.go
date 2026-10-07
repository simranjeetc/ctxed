package tokenize

import (
	"testing"

	tiktoken "github.com/pkoukk/tiktoken-go"
)

// TestTiktokenMethods exercises the Tiktoken wrapper's exported methods against
// a tiny in-memory BPE, so the default test run stays offline and deterministic
// (unlike Resolve, which downloads the real encodings on first use).
func TestTiktokenMethods(t *testing.T) {
	pbe, err := tiktoken.NewCoreBPE(
		map[string]int{"hello": 1, "world": 2},
		map[string]int{"<|endoftext|>": 100},
		`\p{L}+`,
	)
	if err != nil {
		t.Fatalf("NewCoreBPE: %v", err)
	}
	tok := Tiktoken{
		enc:  tiktoken.NewTiktoken(pbe, &tiktoken.Encoding{Name: "cl100k_base"}, map[string]any{}),
		name: "test",
	}

	if tok.Name() != "test" {
		t.Fatalf("Name() = %q, want %q", tok.Name(), "test")
	}
	if tok.Approximate() {
		t.Fatal("Tiktoken must report Approximate() == false")
	}
	if got := tok.Count(""); got != 0 {
		t.Fatalf("Count(\"\") = %d, want 0", got)
	}
	if got := tok.Count("hello world"); got != 2 {
		t.Fatalf("Count(\"hello world\") = %d, want 2", got)
	}
}

package skill

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestInstallWritesToDetectedHarnesses(t *testing.T) {
	home := t.TempDir()
	for _, d := range []string{".claude", ".config/opencode"} {
		if err := os.MkdirAll(filepath.Join(home, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	var out bytes.Buffer
	written, err := Install(home, []byte("SKILL"), &out)
	if err != nil {
		t.Fatal(err)
	}
	if len(written) != 2 {
		t.Fatalf("want 2 harnesses, got %v", written)
	}
	for _, p := range []string{
		".claude/skills/ctxed-overview/SKILL.md",
		".config/opencode/skills/ctxed-overview/SKILL.md",
	} {
		got, err := os.ReadFile(filepath.Join(home, p))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != "SKILL" {
			t.Fatalf("%s = %q, want SKILL", p, got)
		}
	}
}

func TestInstallSkipsMissingHarnesses(t *testing.T) {
	written, err := Install(t.TempDir(), []byte("SKILL"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(written) != 0 {
		t.Fatalf("want no harnesses, got %v", written)
	}
}

func TestInstallOneHarness(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	written, err := Install(home, []byte("SKILL"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(written) != 1 || written[0] != "Claude Code" {
		t.Fatalf("want [Claude Code], got %v", written)
	}
	if _, err := os.Stat(filepath.Join(home, ".config/opencode")); !os.IsNotExist(err) {
		t.Fatal("should not create a harness directory that was absent")
	}
}

func TestInstallRejectsEmpty(t *testing.T) {
	if _, err := Install(t.TempDir(), nil, nil); err == nil {
		t.Fatal("want an error for empty skill content")
	}
}

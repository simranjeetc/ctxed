package harness

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeScript writes an executable POSIX shell script to dir/name and returns
// its path. dir is created if it does not exist.
func writeScript(t *testing.T, dir, name, body string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestOpenCodeBin(t *testing.T) {
	t.Run("env override", func(t *testing.T) {
		bin := writeScript(t, t.TempDir(), "my-opencode", "exit 0")
		t.Setenv("CTXED_OPENCODE_BIN", bin)
		if got, err := OpenCodeBin(); err != nil || got != bin {
			t.Fatalf("got %q, %v", got, err)
		}
	})

	t.Run("PATH lookup", func(t *testing.T) {
		bin := writeScript(t, t.TempDir(), "opencode", "exit 0")
		t.Setenv("CTXED_OPENCODE_BIN", "")
		t.Setenv("PATH", filepath.Dir(bin))
		if got, err := OpenCodeBin(); err != nil || got != bin {
			t.Fatalf("got %q, %v", got, err)
		}
	})

	t.Run("home fallback", func(t *testing.T) {
		home := t.TempDir()
		bin := writeScript(t, filepath.Join(home, ".opencode", "bin"), "opencode", "exit 0")
		t.Setenv("CTXED_OPENCODE_BIN", "")
		t.Setenv("PATH", t.TempDir()) // empty PATH, so LookPath fails
		t.Setenv("HOME", home)
		if got, err := OpenCodeBin(); err != nil || got != bin {
			t.Fatalf("got %q, %v", got, err)
		}
	})
}

func TestExportOpenCode(t *testing.T) {
	ctx := context.Background()

	t.Run("bad session", func(t *testing.T) {
		if _, err := ExportOpenCode(ctx, "not-a-session"); !errors.Is(err, ErrBadOpenCodeSession) {
			t.Fatalf("got %v", err)
		}
	})

	t.Run("success", func(t *testing.T) {
		bin := writeScript(t, t.TempDir(), "opencode", `if [ "$1" = "session" ] && [ "$2" = "export" ]; then
  printf '%s\n' '{"session":"data"}'
fi
exit 0
`)
		t.Setenv("CTXED_OPENCODE_BIN", bin)
		data, err := ExportOpenCode(ctx, "ses_abc")
		if err != nil {
			t.Fatalf("export: %v", err)
		}
		if strings.TrimSpace(string(data)) != `{"session":"data"}` {
			t.Fatalf("data: %q", data)
		}
	})

	t.Run("empty output", func(t *testing.T) {
		bin := writeScript(t, t.TempDir(), "opencode", "exit 0")
		t.Setenv("CTXED_OPENCODE_BIN", bin)
		if _, err := ExportOpenCode(ctx, "ses_abc"); err == nil {
			t.Fatal("want error")
		}
	})

	t.Run("failing binary", func(t *testing.T) {
		bin := writeScript(t, t.TempDir(), "opencode", `echo "boom" >&2
exit 1
`)
		t.Setenv("CTXED_OPENCODE_BIN", bin)
		_, err := ExportOpenCode(ctx, "ses_abc")
		if err == nil || !strings.Contains(err.Error(), "boom") {
			t.Fatalf("got %v", err)
		}
	})
}

func TestOpenCodeRunComplete(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		bin := writeScript(t, t.TempDir(), "opencode", `case "$1" in
  run)
    cat >/dev/null
    printf '%s\n' '{"type":"step_start","sessionID":"ses_fake123"}'
    printf '%s\n' '{"type":"text","sessionID":"ses_fake123","part":{"type":"text","text":"one "}}'
    printf '%s\n' '{"type":"text","sessionID":"ses_fake123","part":{"type":"text","text":"two"}}'
    ;;
  session)
    exit 0
    ;;
esac
`)
		t.Setenv("CTXED_OPENCODE_BIN", bin)
		text, err := (OpenCodeRun{}).Complete(ctx, "the prompt")
		if err != nil {
			t.Fatalf("complete: %v", err)
		}
		if text != "one two" {
			t.Fatalf("text: %q", text)
		}
	})

	t.Run("failing binary", func(t *testing.T) {
		bin := writeScript(t, t.TempDir(), "opencode", `cat >/dev/null
echo "model error" >&2
exit 3
`)
		t.Setenv("CTXED_OPENCODE_BIN", bin)
		_, err := (OpenCodeRun{Model: "x"}).Complete(ctx, "p")
		if err == nil || !strings.Contains(err.Error(), "model error") {
			t.Fatalf("got %v", err)
		}
	})

	t.Run("no text", func(t *testing.T) {
		bin := writeScript(t, t.TempDir(), "opencode", `cat >/dev/null
printf '%s\n' '{"type":"step_start"}'
`)
		t.Setenv("CTXED_OPENCODE_BIN", bin)
		_, err := (OpenCodeRun{}).Complete(ctx, "p")
		if err == nil || !strings.Contains(err.Error(), "no text") {
			t.Fatalf("got %v", err)
		}
	})
}

func TestClaudePrintComplete(t *testing.T) {
	ctx := context.Background()

	t.Run("not found", func(t *testing.T) {
		t.Setenv("CTXED_CLAUDE_BIN", "")
		t.Setenv("PATH", t.TempDir())
		if _, err := (ClaudePrint{}).Complete(ctx, "p"); err == nil || !strings.Contains(err.Error(), "not found") {
			t.Fatalf("got %v", err)
		}
	})

	t.Run("success", func(t *testing.T) {
		bin := writeScript(t, t.TempDir(), "claude", `cat >/dev/null
printf 'categorized output'
`)
		t.Setenv("CTXED_CLAUDE_BIN", bin)
		text, err := (ClaudePrint{}).Complete(ctx, "p")
		if err != nil {
			t.Fatalf("complete: %v", err)
		}
		if text != "categorized output" {
			t.Fatalf("text: %q", text)
		}
	})

	t.Run("no text", func(t *testing.T) {
		bin := writeScript(t, t.TempDir(), "claude", "cat >/dev/null")
		t.Setenv("CTXED_CLAUDE_BIN", bin)
		if _, err := (ClaudePrint{Model: "sonnet"}).Complete(ctx, "p"); err == nil || !strings.Contains(err.Error(), "no text") {
			t.Fatalf("got %v", err)
		}
	})

	t.Run("failing binary", func(t *testing.T) {
		bin := writeScript(t, t.TempDir(), "claude", `cat >/dev/null
echo "claude boom" >&2
exit 4
`)
		t.Setenv("CTXED_CLAUDE_BIN", bin)
		_, err := (ClaudePrint{}).Complete(ctx, "p")
		if err == nil || !strings.Contains(err.Error(), "claude boom") {
			t.Fatalf("got %v", err)
		}
	})
}

func TestClaudeTranscript(t *testing.T) {
	if _, err := ClaudeTranscript("not-a-uuid", ""); !errors.Is(err, ErrBadClaudeSession) {
		t.Fatalf("bad session: got %v", err)
	}

	id := "deadbeef"

	root := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", root)
	proj := claudeProjectDir("/home/u/my-proj")
	dir := filepath.Join(root, "projects", proj)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, id+".jsonl")
	if err := os.WriteFile(want, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if got, err := ClaudeTranscript(id, "/home/u/my-proj"); err != nil || got != want {
		t.Fatalf("cwd: got %q, %v", got, err)
	}
	if got, err := ClaudeTranscript(id, ""); err != nil || got != want {
		t.Fatalf("glob: got %q, %v", got, err)
	}
	if _, err := ClaudeTranscript("cafebabe", "/home/u/my-proj"); err == nil {
		t.Fatal("missing: want error")
	}

	// Default root is $HOME/.claude.
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	hdir := filepath.Join(home, ".claude", "projects", claudeProjectDir("/w/x"))
	if err := os.MkdirAll(hdir, 0o755); err != nil {
		t.Fatal(err)
	}
	hwant := filepath.Join(hdir, id+".jsonl")
	if err := os.WriteFile(hwant, []byte("y\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := ClaudeTranscript(id, "/w/x"); err != nil || got != hwant {
		t.Fatalf("home: got %q, %v", got, err)
	}
}

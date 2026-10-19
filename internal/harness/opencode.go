package harness

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var openCodeSessionRe = regexp.MustCompile(`^ses_[A-Za-z0-9]+$`)

// ErrBadOpenCodeSession reports a session id that cannot safely be passed on.
var ErrBadOpenCodeSession = errors.New("OpenCode session id must be ses_ followed by letters and digits")

// ValidOpenCodeSession checks an OpenCode session id before it is used.
func ValidOpenCodeSession(id string) error {
	if !openCodeSessionRe.MatchString(id) {
		return fmt.Errorf("%w (got %q)", ErrBadOpenCodeSession, id)
	}
	return nil
}

// OpenCodeBin finds the opencode binary: $CTXED_OPENCODE_BIN, PATH, then the usual
// install locations (OpenCode's server runs shell commands with a short PATH).
func OpenCodeBin() (string, error) {
	if b := os.Getenv("CTXED_OPENCODE_BIN"); b != "" {
		return b, nil
	}
	if b, err := exec.LookPath("opencode"); err == nil {
		return b, nil
	}
	home, _ := os.UserHomeDir()
	for _, c := range []string{
		filepath.Join(home, ".opencode", "bin", "opencode"),
		"/opt/homebrew/bin/opencode",
		"/usr/local/bin/opencode",
	} {
		if info, err := os.Stat(c); err == nil && !info.IsDir() {
			return c, nil
		}
	}
	return "", fmt.Errorf("opencode binary not found; set CTXED_OPENCODE_BIN")
}

// ExportOpenCode returns `opencode session export <id>`. Its stdout goes to a temp
// file, not a pipe: piped, a large export is cut short.
func ExportOpenCode(ctx context.Context, sessionID string) ([]byte, error) {
	if err := ValidOpenCodeSession(sessionID); err != nil {
		return nil, err
	}
	bin, err := OpenCodeBin()
	if err != nil {
		return nil, err
	}
	tmp, err := os.CreateTemp("", "ctxed-export-*.json")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	defer func() { _ = tmp.Close() }()
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, bin, "session", "export", sessionID)
	cmd.Stdout = tmp
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("opencode session export %s: %w: %s", sessionID, err, strings.TrimSpace(stderr.String()))
	}
	data, err := os.ReadFile(tmp.Name())
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, fmt.Errorf("opencode session export %s printed nothing", sessionID)
	}
	return data, nil
}

// OpenCodeRun categorizes through `opencode run`, keeping only the text parts of
// its JSON event stream. The throwaway session it creates is deleted after.
//
// Model is the resolved categorizer model. It is never defaulted here: an empty
// Model omits --model and lets OpenCode use the model configured for itself.
type OpenCodeRun struct{ Model string }

// Complete sends the prompt to `opencode run` and returns the text it streams.
func (c OpenCodeRun) Complete(ctx context.Context, prompt string) (string, error) {
	bin, err := OpenCodeBin()
	if err != nil {
		return "", err
	}
	model := strings.TrimSpace(c.Model)
	label := model
	if label == "" {
		label = "harness default"
	}
	args := []string{"run", "--format", "json"}
	if model != "" {
		args = append(args, "--model", model)
	}
	args = append(args, "--title", "ctxed categorize")
	var out, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdin = strings.NewReader(prompt)
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	text, sessionID := parseRunEvents(out.Bytes())
	if sessionID != "" && ValidOpenCodeSession(sessionID) == nil {
		deleteCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		del := exec.CommandContext(deleteCtx, bin, "session", "delete", sessionID)
		del.Stderr = os.Stderr
		if err := del.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "opencode session delete %s: %v\n", sessionID, err)
		}
	}
	if runErr != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			// `opencode run` reports a provider failure as an error event on
			// stdout, with an empty stderr; surface that instead of nothing.
			detail = runEventError(out.Bytes())
		}
		return "", fmt.Errorf("opencode run (%s): %w: %s", label, runErr, detail)
	}
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("opencode run (%s) returned no text", label)
	}
	return text, nil
}

// runEventError returns the message of the first error event in the run's JSON
// stream, so a provider failure that `opencode run` reports on stdout is not
// lost when its stderr is empty.
func runEventError(data []byte) string {
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 1024*1024), 64*1024*1024)
	for sc.Scan() {
		var ev struct {
			Type  string `json:"type"`
			Error struct {
				Message string `json:"message"`
				Type    string `json:"type"`
			} `json:"error"`
		}
		if json.Unmarshal(sc.Bytes(), &ev) != nil {
			continue
		}
		if ev.Type == "error" {
			if msg := strings.TrimSpace(ev.Error.Message); msg != "" {
				return msg
			}
			if t := strings.TrimSpace(ev.Error.Type); t != "" {
				return t
			}
		}
	}
	return ""
}

func parseRunEvents(data []byte) (text, sessionID string) {
	var b strings.Builder
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 1024*1024), 64*1024*1024)
	for sc.Scan() {
		var ev struct {
			Type      string `json:"type"`
			SessionID string `json:"sessionID"`
			Part      struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"part"`
		}
		if json.Unmarshal(sc.Bytes(), &ev) != nil {
			continue
		}
		if sessionID == "" && ev.SessionID != "" {
			sessionID = ev.SessionID
		}
		if ev.Type == "text" && ev.Part.Type == "text" {
			b.WriteString(ev.Part.Text)
		}
	}
	return b.String(), sessionID
}

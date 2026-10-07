package adapter_test

import (
	"strings"
	"testing"

	"github.com/simranjeetc/ctxed/internal/adapter"
	_ "github.com/simranjeetc/ctxed/internal/adapter/builtin"
	"github.com/simranjeetc/ctxed/internal/session"
)

// fakeAdapter proves the registry is the only source of supported formats.
type fakeAdapter struct{}

func (fakeAdapter) Name() string            { return "zz-fake" }
func (fakeAdapter) Detect(data []byte) bool { return strings.HasPrefix(string(data), "FAKE") }
func (fakeAdapter) Parse([]byte) (*session.Document, error) {
	return &session.Document{Source: "zz-fake"}, nil
}
func (fakeAdapter) Write(*session.Document) ([]byte, error) { return []byte("FAKE"), nil }

var minimalOpencode = []byte(`{"info":{"id":"ses_1"},"messages":[{"type":"user","text":"hi"}]}`)

func TestBuiltinAdaptersRegistered(t *testing.T) {
	names := strings.Join(adapter.Names(), ",")
	for _, want := range []string{"opencode", "claude-code"} {
		if !strings.Contains(names, want) {
			t.Fatalf("expected builtin adapter %q, got %q", want, names)
		}
	}
}

func TestDetectPicksOpencode(t *testing.T) {
	a, err := adapter.Detect(minimalOpencode)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if a.Name() != "opencode" {
		t.Fatalf("got %q, want opencode", a.Name())
	}
}

func TestRegisteringAdapterDoesNotChangeExistingBehavior(t *testing.T) {
	before := len(adapter.All())
	adapter.Register(fakeAdapter{})
	after := len(adapter.All())
	if after != before+1 {
		t.Fatalf("registry size %d -> %d, want +1", before, after)
	}
	// The real format still resolves to its own adapter.
	a, err := adapter.Detect(minimalOpencode)
	if err != nil || a.Name() != "opencode" {
		t.Fatalf("detection changed after registering a fake adapter: %v %q", err, a)
	}
	// The fake is now a supported format.
	if a, err := adapter.Detect([]byte("FAKE payload")); err != nil || a.Name() != "zz-fake" {
		t.Fatalf("fake adapter not detected: %v", err)
	}
}

func TestDetectFailureListsSupportedFormats(t *testing.T) {
	_, err := adapter.Detect([]byte("this is not a session"))
	if err == nil {
		t.Fatal("expected an error for an unrecognized format")
	}
	for _, n := range adapter.Names() {
		if !strings.Contains(err.Error(), n) {
			t.Fatalf("error %q does not list supported format %q", err, n)
		}
	}
}

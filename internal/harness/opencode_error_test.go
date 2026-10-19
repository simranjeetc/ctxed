package harness

import "testing"

// A provider failure is reported by `opencode run` as an error event on stdout,
// with an empty stderr. The harness error must carry that message rather than
// swallowing it.
func TestRunEventError(t *testing.T) {
	data := `{"type":"step_start","sessionID":"ses_run1"}
{"type":"error","error":{"type":"provider.no-route","message":"Model unavailable: prov/nope"}}
{"type":"error","error":{"type":"other","message":"second"}}`
	if got := runEventError([]byte(data)); got != "Model unavailable: prov/nope" {
		t.Fatalf("got %q", got)
	}
	if got := runEventError([]byte(`{"type":"text","part":{"type":"text","text":"ok"}}`)); got != "" {
		t.Fatalf("got %q for a stream with no error", got)
	}
}

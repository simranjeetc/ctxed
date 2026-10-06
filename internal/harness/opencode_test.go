package harness

import (
	"errors"
	"testing"
)

func TestParseRunEvents(t *testing.T) {
	data := `{"type":"step_start","sessionID":"ses_run1"}
{"type":"text","sessionID":"ses_run1","part":{"type":"text","text":"{\"categories\":"}}
not json
{"type":"text","part":{"type":"text","text":"[]}"}}
{"type":"text","part":{"type":"reasoning","text":"ignored"}}`
	text, sid := parseRunEvents([]byte(data))
	if text != `{"categories":[]}` || sid != "ses_run1" {
		t.Fatalf("text %q, session %q", text, sid)
	}
}

func TestValidOpenCodeSession(t *testing.T) {
	if err := ValidOpenCodeSession("ses_abc123"); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", "abc", "ses_../x", "ses_a/b"} {
		if err := ValidOpenCodeSession(bad); !errors.Is(err, ErrBadOpenCodeSession) {
			t.Fatalf("%q: got %v", bad, err)
		}
	}
}

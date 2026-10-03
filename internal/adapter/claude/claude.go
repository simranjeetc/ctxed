// Package claude adapts Claude Code JSONL transcripts under
// ~/.claude/projects/<project>/<session>.jsonl.
package claude

import (
	"encoding/json"
	"strings"

	"github.com/simranjeetc/ctxed/internal/adapter"
	"github.com/simranjeetc/ctxed/internal/session"
)

func init() { adapter.Register(&Adapter{}) }

// Adapter implements adapter.Adapter for Claude Code transcripts.
type Adapter struct{}

func (a *Adapter) Name() string { return "claude-code" }

// Detect reports whether data is a Claude Code transcript: line-oriented JSON
// whose first non-empty line is an object carrying a type and one of the
// Claude-specific envelope fields.
func (a *Adapter) Detect(data []byte) bool {
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var probe map[string]json.RawMessage
		if json.Unmarshal([]byte(line), &probe) != nil {
			return false
		}
		_, hasType := probe["type"]
		_, hasMsg := probe["message"]
		_, hasSession := probe["sessionId"]
		_, hasUUID := probe["uuid"]
		return hasType && (hasMsg || hasSession || hasUUID)
	}
	return false
}

type contentPart struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	Thinking  string          `json:"thinking"`
	ID        string          `json:"id"`
	ToolUseID string          `json:"tool_use_id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	Content   json.RawMessage `json:"content"`
}

type line struct {
	Type    string `json:"type"`
	UUID    string `json:"uuid"`
	Message *struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

// Parse converts a Claude Code transcript into a Document. Non-conversation
// lines are retained verbatim so they survive a write.
func (a *Adapter) Parse(data []byte) (*session.Document, error) {
	doc := &session.Document{Source: a.Name(), Format: session.FormatJSONL}
	for _, raw := range strings.Split(string(data), "\n") {
		item := session.RawLine{Raw: raw, EntryIndex: -1}
		if strings.TrimSpace(raw) != "" {
			var l line
			if json.Unmarshal([]byte(raw), &l) == nil && l.Message != nil && isEntry(l.Type) {
				e := &session.Entry{ID: l.UUID, Role: l.Message.Role, Raw: json.RawMessage(raw)}
				if e.Role == "" {
					e.Role = l.Type
				}
				e.Text, e.Kind, e.CallIDs, e.ResultIDs = extract(l.Message.Content)
				doc.Add(e)
				item.EntryIndex = e.Index
			}
		}
		doc.Lines = append(doc.Lines, item)
	}
	doc.AssignFallbackIDs()
	return doc, nil
}

// Write rebuilds the transcript, preserving non-entry lines and their order.
func (a *Adapter) Write(doc *session.Document) ([]byte, error) {
	var b strings.Builder
	for _, it := range doc.Lines {
		if it.Dropped {
			continue
		}
		b.WriteString(it.Raw)
		b.WriteString("\n")
	}
	return []byte(b.String()), nil
}

func isEntry(t string) bool { return t == "user" || t == "assistant" }

func extract(raw json.RawMessage) (string, session.Kind, []string, []string) {
	if len(raw) == 0 {
		return "", session.KindMessage, nil, nil
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.TrimSpace(s), session.KindMessage, nil, nil
	}
	var parts []contentPart
	if json.Unmarshal(raw, &parts) != nil {
		return strings.TrimSpace(string(raw)), session.KindMessage, nil, nil
	}

	var b strings.Builder
	add := func(s string) {
		if s == "" {
			return
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(s)
	}
	kind := session.KindMessage
	var calls, results []string
	for _, p := range parts {
		switch p.Type {
		case "text":
			add(p.Text)
		case "thinking":
			add(p.Thinking)
			if kind == session.KindMessage {
				kind = session.KindReasoning
			}
		case "tool_use":
			kind = session.KindToolCall
			if p.ID != "" {
				calls = append(calls, p.ID)
			}
			add(p.Name)
			add(string(p.Input))
		case "tool_result":
			kind = session.KindToolResult
			if p.ToolUseID != "" {
				results = append(results, p.ToolUseID)
			}
			add(stringify(p.Content))
		default:
			add(p.Text)
		}
	}
	return strings.TrimSpace(b.String()), kind, calls, results
}

func stringify(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var parts []contentPart
	if json.Unmarshal(raw, &parts) == nil {
		var b strings.Builder
		for _, p := range parts {
			if p.Text != "" {
				if b.Len() > 0 {
					b.WriteString(" ")
				}
				b.WriteString(p.Text)
			}
		}
		return b.String()
	}
	return string(raw)
}

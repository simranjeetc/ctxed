// Package claude adapts Claude Code JSONL transcripts under
// ~/.claude/projects/<project>/<session>.jsonl.
//
// # Compaction
//
// Claude Code never rewrites a transcript when it compacts; it appends. Seen in
// real transcripts (Claude Code 2.1.288 and 2.1.291, manual /compact):
//
//   - a boundary line {"type":"system","subtype":"compact_boundary",
//     "parentUuid":null,"logicalParentUuid":<last pre-compaction line>,
//     "compactMetadata":{"trigger","preTokens","postTokens",…}};
//   - then a user line with "isCompactSummary":true holding the summary as a
//     string (also "isVisibleInTranscriptOnly":true), parented to the boundary;
//   - then the /compact command's own lines (local-command caveat, command,
//     stdout) and the conversation that follows.
//
// From 2.1.291 the boundary may also keep a tail of the old conversation live:
// compactMetadata.preservedMessages.uuids lists lines from before the boundary
// (here the last assistant turn) that Claude Code re-attaches after the summary.
// Older versions write no preservedMessages and keep nothing.
//
// So the live context is the summary, then the preserved lines, then every
// entry after the last boundary. Parse exposes only those as Entries; the
// earlier lines are kept verbatim like any non-entry line, so every line is
// still written back.
//
// Open questions, answered from those transcripts:
//
//   - No pre-compaction content is flushed after a boundary. The lines after it
//     that carry earlier timestamps are the summary and the /compact command
//     lines, which belong to the compaction itself.
//   - Subagent sidechains are not in the main transcript: they are written to
//     <session>/subagents/agent-*.jsonl, so no sidechain handling is needed.
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
	Type             string `json:"type"`
	Subtype          string `json:"subtype"`
	UUID             string `json:"uuid"`
	IsCompactSummary bool   `json:"isCompactSummary"`
	Message          *struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
	CompactMetadata *struct {
		PreservedMessages *struct {
			UUIDs []string `json:"uuids"`
		} `json:"preservedMessages"`
	} `json:"compactMetadata"`
}

func (l *line) isBoundary() bool { return l.Type == "system" && l.Subtype == "compact_boundary" }

// Parse converts a Claude Code transcript into a Document. Non-conversation
// lines are retained verbatim so they survive a write. A compacted transcript
// exposes only its live context as Entries (see the package comment).
func (a *Adapter) Parse(data []byte) (*session.Document, error) {
	doc := &session.Document{Source: a.Name(), Format: session.FormatJSONL}
	raws := strings.Split(string(data), "\n")
	parsed := make([]*line, len(raws))
	boundary := -1
	for i, raw := range raws {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		var l line
		if json.Unmarshal([]byte(raw), &l) != nil {
			continue
		}
		parsed[i] = &l
		if l.isBoundary() {
			boundary = i
		}
	}
	preserved := map[string]bool{}
	if boundary >= 0 {
		if m := parsed[boundary].CompactMetadata; m != nil && m.PreservedMessages != nil {
			for _, u := range m.PreservedMessages.UUIDs {
				preserved[u] = true
			}
		}
	}

	// Collect the live entries, then add them in the order the model sees
	// them: summary, preserved lines, the rest.
	var summary, kept, after []*session.Entry
	lineOf := map[*session.Entry]int{}
	for i, raw := range raws {
		doc.Lines = append(doc.Lines, session.RawLine{Raw: raw, EntryIndex: -1})
		l := parsed[i]
		if l == nil || l.Message == nil || !isEntry(l.Type) {
			continue
		}
		if i < boundary && !preserved[l.UUID] {
			doc.Compacted++
			continue // compacted away: kept verbatim, never an entry
		}
		e := &session.Entry{ID: l.UUID, Role: l.Message.Role, Raw: json.RawMessage(raw)}
		if e.Role == "" {
			e.Role = l.Type
		}
		e.Text, e.Kind, e.CallIDs, e.ResultIDs = extract(l.Message.Content)
		lineOf[e] = i
		switch {
		case i > boundary && l.IsCompactSummary && summary == nil:
			e.Kind = session.KindSummary
			summary = append(summary, e)
		case i > boundary:
			after = append(after, e)
		default:
			kept = append(kept, e)
		}
	}
	for _, group := range [][]*session.Entry{summary, kept, after} {
		for _, e := range group {
			doc.Add(e)
			doc.Lines[lineOf[e]].EntryIndex = e.Index
		}
	}
	doc.AssignFallbackIDs()
	return doc, nil
}

// Write rebuilds the transcript, preserving non-entry lines and their order.
// Lines are rejoined exactly as Parse split them, so an unedited document is
// written back byte-for-byte (including the final newline).
func (a *Adapter) Write(doc *session.Document) ([]byte, error) {
	kept := make([]string, 0, len(doc.Lines))
	for _, it := range doc.Lines {
		if !it.Dropped {
			kept = append(kept, it.Raw)
		}
	}
	return []byte(strings.Join(kept, "\n")), nil
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

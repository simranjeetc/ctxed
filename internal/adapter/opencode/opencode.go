// Package opencode adapts the JSON produced by `opencode session export`.
package opencode

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/simranjeetc/ctxed/internal/adapter"
	"github.com/simranjeetc/ctxed/internal/session"
)

func init() { adapter.Register(&Adapter{}) }

// Adapter implements adapter.Adapter for OpenCode session exports.
type Adapter struct{}

func (a *Adapter) Name() string { return "opencode" }

// Detect reports whether data is an OpenCode export: a JSON object with a
// messages array, plus either an info object or message items that carry a type.
func (a *Adapter) Detect(data []byte) bool {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		return false
	}
	rawMsgs, ok := top["messages"]
	if !ok {
		return false
	}
	var arr []json.RawMessage
	if err := json.Unmarshal(rawMsgs, &arr); err != nil {
		return false
	}
	if _, ok := top["info"]; ok {
		return true
	}
	for _, m := range arr {
		var p struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(m, &p) == nil && p.Type != "" {
			return true
		}
	}
	return false
}

type message struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	Text        string `json:"text"`
	Description string `json:"description"`
	Content     []struct {
		Type  string          `json:"type"`
		Text  string          `json:"text"`
		ID    string          `json:"id"`
		Name  string          `json:"name"`
		State json.RawMessage `json:"state"`
	} `json:"content"`
}

// Parse converts an OpenCode export into a Document.
func (a *Adapter) Parse(data []byte) (*session.Document, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		return nil, fmt.Errorf("opencode: not a JSON object: %w", err)
	}
	rawMsgs, ok := top["messages"]
	if !ok {
		return nil, fmt.Errorf("opencode: no messages array")
	}
	var arr []json.RawMessage
	if err := json.Unmarshal(rawMsgs, &arr); err != nil {
		return nil, fmt.Errorf("opencode: messages is not an array: %w", err)
	}

	doc := &session.Document{Source: a.Name(), Format: session.FormatJSON, Top: top, EntriesKey: "messages"}
	for _, raw := range arr {
		var m message
		_ = json.Unmarshal(raw, &m)
		item := session.RawItem{Raw: raw, EntryIndex: -1}
		if isEntry(m.Type) {
			e := &session.Entry{ID: m.ID, Role: m.Type, Raw: raw}
			e.Text, e.Kind, e.CallIDs, e.ResultIDs = extract(m)
			doc.Add(e)
			item.EntryIndex = e.Index
		}
		doc.Items = append(doc.Items, item)
	}
	doc.AssignFallbackIDs()
	return doc, nil
}

// Write rebuilds an OpenCode export, preserving every top-level field and every
// message the edit did not target.
func (a *Adapter) Write(doc *session.Document) ([]byte, error) {
	items := make([]json.RawMessage, 0, len(doc.Items))
	for _, it := range doc.Items {
		if it.Dropped {
			continue
		}
		items = append(items, it.Raw)
	}
	blob, err := json.Marshal(items)
	if err != nil {
		return nil, err
	}
	top := make(map[string]json.RawMessage, len(doc.Top))
	for k, v := range doc.Top {
		top[k] = v
	}
	top[doc.EntriesKey] = blob
	return json.MarshalIndent(top, "", "  ")
}

func isEntry(t string) bool { return t == "user" || t == "assistant" || t == "system" }

func extract(m message) (string, session.Kind, []string, []string) {
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

	add(m.Text)
	add(m.Description)
	for _, p := range m.Content {
		switch p.Type {
		case "text":
			add(p.Text)
		case "reasoning":
			add(p.Text)
			if kind == session.KindMessage {
				kind = session.KindReasoning
			}
		case "tool":
			// An OpenCode tool part carries both the call and its result, so the
			// entry both issues and answers the id, and can never orphan itself.
			kind = session.KindToolCall
			if p.ID != "" {
				calls = append(calls, p.ID)
				results = append(results, p.ID)
			}
			add(p.Name)
			add(string(p.State))
		}
	}
	return strings.TrimSpace(b.String()), kind, calls, results
}

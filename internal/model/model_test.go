package model_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/simranjeetc/ctxed/internal/model"
)

func TestCommandClientReturnsStdout(t *testing.T) {
	c, err := model.Resolve(model.Config{Command: "printf 'hello from cmd'"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := c.Complete(context.Background(), "ignored prompt")
	if err != nil {
		t.Fatal(err)
	}
	if got != "hello from cmd" {
		t.Fatalf("got %q", got)
	}
}

func TestCommandOverrideTakesPrecedence(t *testing.T) {
	c, err := model.Resolve(model.Config{
		Command: "printf cmd-wins",
		BaseURL: "http://127.0.0.1:1", // unreachable: must not be used
		APIKey:  "k",
		Model:   "m",
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := c.Complete(context.Background(), "p")
	if err != nil {
		t.Fatalf("command override should not touch the endpoint: %v", err)
	}
	if got != "cmd-wins" {
		t.Fatalf("got %q", got)
	}
}

func TestCommandFailureAborts(t *testing.T) {
	c, err := model.Resolve(model.Config{Command: "exit 3"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Complete(context.Background(), "p"); err == nil {
		t.Fatal("expected an error from a failing command")
	}
}

func TestResolveMissingConfig(t *testing.T) {
	if _, err := model.Resolve(model.Config{}); err == nil {
		t.Fatal("expected an error when nothing is configured")
	}
	if _, err := model.Resolve(model.Config{BaseURL: "http://x"}); err == nil {
		t.Fatal("expected an error when the API key and model are missing")
	}
}

func TestOpenAIClient(t *testing.T) {
	var gotPath, gotAuth string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"model says hi"}}]}`)
	}))
	defer srv.Close()

	c, err := model.Resolve(model.Config{BaseURL: srv.URL, APIKey: "secret", Model: "gpt-test"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := c.Complete(context.Background(), "the prompt")
	if err != nil {
		t.Fatal(err)
	}
	if got != "model says hi" {
		t.Fatalf("got %q", got)
	}
	if gotPath != "/chat/completions" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotAuth != "Bearer secret" {
		t.Fatalf("auth = %q", gotAuth)
	}
	if gotBody["model"] != "gpt-test" {
		t.Fatalf("model in body = %v", gotBody["model"])
	}
	msgs, _ := gotBody["messages"].([]any)
	if len(msgs) != 1 {
		t.Fatalf("messages = %v", gotBody["messages"])
	}
}

func TestOpenAIErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusUnauthorized)
	}))
	defer srv.Close()
	c, _ := model.Resolve(model.Config{BaseURL: srv.URL, APIKey: "k", Model: "m"})
	if _, err := c.Complete(context.Background(), "p"); err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("expected a 401 error, got %v", err)
	}
}

// Package model reaches a model for categorization: an OpenAI-compatible
// endpoint by default, or a caller-named command that takes the prompt on stdin
// and returns the response on stdout.
package model

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

// Client is a single prompt-in, text-out model call.
type Client interface {
	Complete(ctx context.Context, prompt string) (string, error)
}

// Config selects a transport. Command, when set, takes precedence over the
// endpoint fields.
type Config struct {
	BaseURL string
	APIKey  string
	Model   string
	Command string
}

// Resolve returns the configured client, or an error naming both options when
// neither is usable.
func Resolve(cfg Config) (Client, error) {
	if strings.TrimSpace(cfg.Command) != "" {
		return &commandClient{cmd: cfg.Command}, nil
	}
	missing := []string{}
	if cfg.BaseURL == "" {
		missing = append(missing, "base URL")
	}
	if cfg.APIKey == "" {
		missing = append(missing, "API key")
	}
	if cfg.Model == "" {
		missing = append(missing, "model")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("no model configured (missing %s); set --categorizer-cmd, or --base-url/--api-key/--model", strings.Join(missing, ", "))
	}
	return &openAIClient{
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
		apiKey:  cfg.APIKey,
		model:   cfg.Model,
		http:    &http.Client{Timeout: 120 * time.Second},
	}, nil
}

// commandClient runs a shell command with the prompt on stdin and returns its
// stdout as the response.
type commandClient struct{ cmd string }

func (c *commandClient) Complete(ctx context.Context, prompt string) (string, error) {
	cmd := exec.CommandContext(ctx, "sh", "-c", c.cmd)
	cmd.Stdin = strings.NewReader(prompt)
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("categorizer command failed: %v: %s", err, strings.TrimSpace(errBuf.String()))
	}
	return out.String(), nil
}

// openAIClient speaks the OpenAI chat-completions shape.
type openAIClient struct {
	baseURL string
	apiKey  string
	model   string
	http    *http.Client
}

func (c *openAIClient) Complete(ctx context.Context, prompt string) (string, error) {
	body, err := json.Marshal(map[string]any{
		"model":       c.model,
		"temperature": 0,
		"messages":    []map[string]string{{"role": "user", "content": prompt}},
	})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("model request: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("model endpoint returned %s: %s", resp.Status, strings.TrimSpace(string(raw)))
	}
	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", fmt.Errorf("model response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("model returned no choices")
	}
	return parsed.Choices[0].Message.Content, nil
}

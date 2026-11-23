package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Usage reports what a completion cost.
type Usage struct {
	PromptTokens     int
	CompletionTokens int
	PromptEval       time.Duration // time spent processing the prompt (near zero when the prefix was cached)
	Total            time.Duration
}

// Provider is a chat-completion backend.
type Provider interface {
	Complete(ctx context.Context, system, user string) (string, Usage, error)
	Name() string
}

// ErrModelNotFound means the server doesn't have the configured model.
var ErrModelNotFound = errors.New("model not found")

// ---- Ollama (native API: lets us set num_ctx and enforce a JSON schema) ----

// Ollama talks to Ollama's /api/chat.
type Ollama struct {
	BaseURL   string // e.g. http://localhost:11434
	Model     string
	NumCtx    int
	KeepAlive string
	Client    *http.Client
}

func (o *Ollama) Name() string { return "ollama/" + o.Model }

func (o *Ollama) Complete(ctx context.Context, system, user string) (string, Usage, error) {
	body := map[string]any{
		"model":  o.Model,
		"stream": false,
		"format": responseSchema,
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
		"options":    map[string]any{"temperature": 0, "num_ctx": o.NumCtx},
		"keep_alive": o.KeepAlive,
	}
	var out struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		PromptEvalCount    int    `json:"prompt_eval_count"`
		PromptEvalDuration int64  `json:"prompt_eval_duration"`
		EvalCount          int    `json:"eval_count"`
		TotalDuration      int64  `json:"total_duration"`
		Error              string `json:"error"`
	}
	status, err := postJSON(ctx, o.Client, strings.TrimSuffix(o.BaseURL, "/")+"/api/chat", "", body, &out)
	if status == http.StatusNotFound || strings.Contains(out.Error, "not found") {
		return "", Usage{}, fmt.Errorf("%w: %q (run: ollama pull %s)", ErrModelNotFound, o.Model, o.Model)
	}
	if err != nil {
		return "", Usage{}, err
	}
	if out.Error != "" {
		return "", Usage{}, errors.New(out.Error)
	}
	return out.Message.Content, Usage{
		PromptTokens:     out.PromptEvalCount,
		CompletionTokens: out.EvalCount,
		PromptEval:       time.Duration(out.PromptEvalDuration),
		Total:            time.Duration(out.TotalDuration),
	}, nil
}

// ---- OpenAI-compatible (LM Studio, llama.cpp server, vLLM, LocalAI, ...) ----

// OpenAI talks to any OpenAI-compatible /chat/completions endpoint.
type OpenAI struct {
	BaseURL string // including /v1, e.g. http://localhost:1234/v1
	Model   string
	APIKey  string
	Client  *http.Client
}

func (o *OpenAI) Name() string { return "openai-compatible/" + o.Model }

func (o *OpenAI) Complete(ctx context.Context, system, user string) (string, Usage, error) {
	body := map[string]any{
		"model":       o.Model,
		"temperature": 0,
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
		"response_format": map[string]string{"type": "json_object"},
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	endpoint := strings.TrimSuffix(o.BaseURL, "/") + "/chat/completions"
	start := time.Now()
	status, err := postJSON(ctx, o.Client, endpoint, o.APIKey, body, &out)
	if status == http.StatusBadRequest {
		// Some servers reject response_format; retry without it.
		delete(body, "response_format")
		status, err = postJSON(ctx, o.Client, endpoint, o.APIKey, body, &out)
	}
	if status == http.StatusNotFound {
		return "", Usage{}, fmt.Errorf("%w: %q at %s", ErrModelNotFound, o.Model, o.BaseURL)
	}
	if err != nil {
		return "", Usage{}, err
	}
	if len(out.Choices) == 0 {
		return "", Usage{}, errors.New("empty response from server")
	}
	return out.Choices[0].Message.Content, Usage{
		PromptTokens:     out.Usage.PromptTokens,
		CompletionTokens: out.Usage.CompletionTokens,
		Total:            time.Since(start),
	}, nil
}

func postJSON(ctx context.Context, c *http.Client, endpoint, apiKey string, body, out any) (int, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	if c == nil {
		c = http.DefaultClient
	}
	resp, err := c.Do(req)
	if err != nil {
		return 0, fmt.Errorf("cannot reach %s: %w", hostOf(endpoint), err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return resp.StatusCode, err
	}
	// Decode even on error statuses: Ollama puts {"error": ...} in the body.
	decErr := json.Unmarshal(raw, out)
	if resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(raw))
		if len(msg) > 300 {
			msg = msg[:300]
		}
		return resp.StatusCode, fmt.Errorf("%s returned %s: %s", hostOf(endpoint), resp.Status, msg)
	}
	if decErr != nil {
		return resp.StatusCode, fmt.Errorf("decoding response: %w", decErr)
	}
	return resp.StatusCode, nil
}

func hostOf(endpoint string) string {
	if u, err := url.Parse(endpoint); err == nil {
		return u.Host
	}
	return endpoint
}

// IsLocal reports whether the endpoint is on this machine, used to warn
// before sending query text elsewhere.
func IsLocal(endpoint string) bool {
	u, err := url.Parse(endpoint)
	if err != nil {
		return false
	}
	switch h := u.Hostname(); h {
	case "localhost", "127.0.0.1", "::1", "host.docker.internal":
		return true
	default:
		return strings.HasSuffix(h, ".localhost")
	}
}

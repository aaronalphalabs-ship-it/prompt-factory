// Package provider calls OpenAI-compatible chat completion APIs.
package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/aaronalphalabs-ship-it/prompt-factory/internal/config"
)

type Client struct {
	P           config.Provider
	Temperature float64
	HTTP        *http.Client
}

func New(p config.Provider, temp float64) *Client {
	return &Client{P: p, Temperature: temp, HTTP: &http.Client{Timeout: 120 * time.Second}}
}

type Result struct {
	Text     string        `json:"text"`
	Model    string        `json:"model"`
	Provider string        `json:"provider"`
	Tokens   int           `json:"tokens"`
	Latency  time.Duration `json:"latency_ns"`
}

type msg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Complete sends one user message (with optional system message) and
// retries transient failures (429/5xx) up to 3 times with backoff.
func (c *Client) Complete(ctx context.Context, system, user string) (*Result, error) {
	msgs := []msg{}
	if strings.TrimSpace(system) != "" {
		msgs = append(msgs, msg{"system", system})
	}
	msgs = append(msgs, msg{"user", user})
	body, _ := json.Marshal(map[string]any{
		"model":       c.P.Model,
		"messages":    msgs,
		"temperature": c.Temperature,
	})

	var lastErr error
	for attempt := 0; attempt < 4; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(1<<attempt) * 500 * time.Millisecond):
			}
		}
		res, retry, err := c.do(ctx, body)
		if err == nil {
			return res, nil
		}
		lastErr = err
		if !retry {
			break
		}
	}
	return nil, lastErr
}

func (c *Client) do(ctx context.Context, body []byte) (*Result, bool, error) {
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(c.P.BaseURL, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.P.APIKeyEnv != "" {
		key := os.Getenv(c.P.APIKeyEnv)
		if key == "" {
			return nil, false, fmt.Errorf("%s is not set (provider %s)", c.P.APIKeyEnv, c.P.Name)
		}
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, true, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == 429 || resp.StatusCode >= 500 {
		return nil, true, fmt.Errorf("%s: HTTP %d: %s", c.P.Name, resp.StatusCode, trim(raw))
	}
	if resp.StatusCode >= 300 {
		return nil, false, fmt.Errorf("%s: HTTP %d: %s", c.P.Name, resp.StatusCode, trim(raw))
	}
	var out struct {
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			TotalTokens int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, false, fmt.Errorf("%s: bad JSON: %s", c.P.Name, trim(raw))
	}
	if len(out.Choices) == 0 {
		return nil, false, fmt.Errorf("%s: empty response", c.P.Name)
	}
	if strings.TrimSpace(out.Choices[0].Message.Content) == "" {
		return nil, true, fmt.Errorf("%s: model returned no text (reasoning models may need a retry)", c.P.Name)
	}
	model := out.Model
	if model == "" {
		model = c.P.Model
	}
	return &Result{
		Text:     strings.TrimSpace(out.Choices[0].Message.Content),
		Model:    model,
		Provider: c.P.Name,
		Tokens:   out.Usage.TotalTokens,
		Latency:  time.Since(start),
	}, false, nil
}

func trim(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 300 {
		s = s[:300] + "…"
	}
	return s
}

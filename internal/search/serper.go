// Package search provides web search for AI providers that lack a built-in
// server-side search tool (e.g. Kimi via Moonshot). Backed by Serper.dev.
package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const serperURL = "https://google.serper.dev/search"

// Serper runs a Google search via Serper.dev and formats the top results as
// plain text suitable for an LLM tool result. num caps organic results.
func Serper(ctx context.Context, apiKey, query string, num int) (string, error) {
	if num <= 0 {
		num = 8
	}
	payload, _ := json.Marshal(map[string]any{"q": query, "num": num})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, serperURL, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("X-API-KEY", apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("serper: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("serper: HTTP %d: %.200s", resp.StatusCode, string(body))
	}

	var out struct {
		AnswerBox struct {
			Title   string `json:"title"`
			Answer  string `json:"answer"`
			Snippet string `json:"snippet"`
		} `json:"answerBox"`
		KnowledgeGraph struct {
			Title       string `json:"title"`
			Type        string `json:"type"`
			Description string `json:"description"`
		} `json:"knowledgeGraph"`
		Organic []struct {
			Title   string `json:"title"`
			Link    string `json:"link"`
			Snippet string `json:"snippet"`
		} `json:"organic"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("serper: parse response: %w", err)
	}

	var b strings.Builder
	if a := out.AnswerBox; a.Answer != "" || a.Snippet != "" {
		fmt.Fprintf(&b, "Answer box: %s %s %s\n\n", a.Title, a.Answer, a.Snippet)
	}
	if k := out.KnowledgeGraph; k.Title != "" {
		fmt.Fprintf(&b, "Knowledge graph: %s (%s) — %s\n\n", k.Title, k.Type, k.Description)
	}
	if len(out.Organic) == 0 && b.Len() == 0 {
		return "No results found.", nil
	}
	for i, r := range out.Organic {
		if i >= num {
			break
		}
		fmt.Fprintf(&b, "%d. %s\n   %s\n   %s\n", i+1, r.Title, r.Link, r.Snippet)
	}
	return strings.TrimSpace(b.String()), nil
}

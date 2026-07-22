package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// Moonshot serves an Anthropic-compatible Messages API, so the Kimi provider
// reuses the same SDK with a different base URL. Its one hard difference:
// there is no server-side web_search tool, so the Kimi path carries a
// client-side web_search tool backed by Serper.dev (see runCustomTools).
const (
	kimiBaseURL      = "https://api.moonshot.ai/anthropic"
	defaultKimiModel = "kimi-k2.5"

	// Settings keys (per-user, stored via the Settings page; secrets encrypted).
	SettingAIProvider   = "ai_provider" // "claude" (default) | "kimi"
	SettingKimiAPIKey   = "kimi_api_key"
	SettingKimiModel    = "kimi_model"
	SettingSerperAPIKey = "serper_api_key"
)

// SettingsSource reads per-user settings (implemented by store.Store).
type SettingsSource interface {
	GetSetting(ctx context.Context, userID int64, key string) (string, bool, error)
}

// Decrypter decrypts secret setting values (implemented by crypto.Cipher).
type Decrypter interface {
	Decrypt(string) (string, error)
}

// ConfigureProviders wires the per-user settings lookup that alternate
// providers (Kimi) need for their API keys. Without it only Claude works.
func (g *Generator) ConfigureProviders(settings SettingsSource, dec Decrypter) {
	g.settings = settings
	g.dec = dec
}

// providerRun is the resolved per-generation provider configuration.
type providerRun struct {
	name      string // "claude" | "kimi"
	client    anthropic.Client
	model     anthropic.Model
	serperKey string // non-empty => web_search is a client-side Serper-backed tool
}

// providerFor resolves which model backend a generation should use.
// in.Provider selects it ("kimi"); anything else is the default Claude path.
func (g *Generator) providerFor(ctx context.Context, in Input) (providerRun, error) {
	if !strings.EqualFold(strings.TrimSpace(in.Provider), "kimi") {
		if !g.enabled {
			return providerRun{}, errDisabled
		}
		return providerRun{name: "claude", client: g.client, model: g.model}, nil
	}

	if g.settings == nil || g.dec == nil {
		return providerRun{}, errors.New("kimi provider selected but provider settings are not wired up")
	}
	userID := in.Lead.UserID

	secret := func(key string) (string, error) {
		enc, ok, err := g.settings.GetSetting(ctx, userID, key)
		if err != nil {
			return "", err
		}
		if !ok || strings.TrimSpace(enc) == "" {
			return "", nil
		}
		return g.dec.Decrypt(enc)
	}

	apiKey, err := secret(SettingKimiAPIKey)
	if err != nil {
		return providerRun{}, fmt.Errorf("kimi api key: %w", err)
	}
	if apiKey == "" {
		return providerRun{}, errors.New("kimi provider selected but no Kimi API key is set — add it in Settings → AI generation")
	}
	serperKey, err := secret(SettingSerperAPIKey)
	if err != nil {
		return providerRun{}, fmt.Errorf("serper api key: %w", err)
	}
	if serperKey == "" {
		return providerRun{}, errors.New("kimi provider needs a Serper.dev API key for web research — add it in Settings → AI generation")
	}

	model := defaultKimiModel
	if v, ok, _ := g.settings.GetSetting(ctx, userID, SettingKimiModel); ok && strings.TrimSpace(v) != "" {
		model = strings.TrimSpace(v)
	}

	return providerRun{
		name:      "kimi",
		client:    anthropic.NewClient(option.WithAPIKey(apiKey), option.WithBaseURL(kimiBaseURL)),
		model:     anthropic.Model(model),
		serperKey: serperKey,
	}, nil
}

// kimiModelsURL is Moonshot's OpenAI-compatible model listing endpoint — the
// Anthropic-compatible surface has no models endpoint, so valid model IDs for
// a given key can only be discovered here.
const kimiModelsURL = "https://api.moonshot.ai/v1/models"

// ListKimiModels returns the model IDs the given Moonshot API key can use.
func ListKimiModels(ctx context.Context, apiKey string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, kimiModelsURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %.200s", resp.StatusCode, string(body))
	}
	var out struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("parse models list: %w", err)
	}
	models := make([]string, 0, len(out.Data))
	for _, m := range out.Data {
		if m.ID != "" {
			models = append(models, m.ID)
		}
	}
	sort.Strings(models)
	return models, nil
}

// VerifyKimiKey makes a minimal live request against Moonshot's
// Anthropic-compatible endpoint to confirm the API key (and model name) work.
// Returns the model's reply text and the model that was used.
func VerifyKimiKey(ctx context.Context, apiKey, model string) (reply, usedModel string, err error) {
	if strings.TrimSpace(model) == "" {
		model = defaultKimiModel
	}
	client := anthropic.NewClient(option.WithAPIKey(apiKey), option.WithBaseURL(kimiBaseURL))
	resp, err := client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(model),
		MaxTokens: 16,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock("Reply with the single word: ok")),
		},
	})
	if err != nil {
		return "", model, err
	}
	var text strings.Builder
	for _, b := range resp.Content {
		if tb, ok := b.AsAny().(anthropic.TextBlock); ok {
			text.WriteString(tb.Text)
		}
	}
	return strings.TrimSpace(text.String()), model, nil
}

// clientWebSearchTool is the client-side replacement for Anthropic's
// server-side web_search, used on providers that don't have one.
func clientWebSearchTool() anthropic.ToolUnionParam {
	return anthropic.ToolUnionParam{OfTool: &anthropic.ToolParam{
		Name:        "web_search",
		Description: anthropic.String("Search the web (Google). Returns the top results as 'title / URL / snippet' lines. Use it for all brand research steps in the decision tree."),
		InputSchema: anthropic.ToolInputSchemaParam{
			Properties: map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "The search query.",
				},
			},
			Required: []string{"query"},
		},
	}}
}

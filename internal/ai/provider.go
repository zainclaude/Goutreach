package ai

import (
	"context"
	"errors"
	"fmt"
	"strings"

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

package embeddings

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestOpenAICompatibleClientWithRealEndpointFromLLMSYAML(t *testing.T) {
	if os.Getenv("SPEECDEX_REAL_EMBEDDING_TEST") != "1" {
		t.Skip("set SPEECDEX_REAL_EMBEDDING_TEST=1 to run against the real endpoint in llms.yaml")
	}

	cfg := readRealEmbeddingConfig(t)
	if cfg.Style != "openai-compatible" {
		t.Skipf("llms.yaml embedding style is %q, want openai-compatible", cfg.Style)
	}

	client, err := NewOpenAICompatibleClient(OpenAICompatibleOptions{
		Endpoint:   cfg.Endpoint,
		ModelName:  firstNonEmpty(cfg.ModelName, cfg.Name),
		APIKey:     firstNonEmpty(cfg.APIKey, cfg.APIKeyLegacy),
		Dimensions: cfg.DefaultDims,
	})
	if err != nil {
		t.Fatalf("NewOpenAICompatibleClient() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	vectors, err := client.Embed(ctx, []string{
		"Speecdex indexes local Markdown documents.",
		"OpenAI-compatible embedding APIs return ordered vectors.",
	})
	if err != nil {
		t.Fatalf("Embed() error = %v", err)
	}

	if len(vectors) != 2 {
		t.Fatalf("vectors length = %d, want 2", len(vectors))
	}
	for index, vector := range vectors {
		if len(vector) != cfg.DefaultDims {
			t.Fatalf("vector[%d] dimensions = %d, want %d", index, len(vector), cfg.DefaultDims)
		}
	}
}

func readRealEmbeddingConfig(t *testing.T) realEmbeddingConfig {
	t.Helper()

	path := filepath.Join("..", "..", "llms.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}

	var doc realLLMSYAML
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("Unmarshal(%q) error = %v", path, err)
	}
	return doc.LLMs.Embedding
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

type realLLMSYAML struct {
	LLMs struct {
		Embedding realEmbeddingConfig `yaml:"embedding"`
	} `yaml:"llms"`
}

type realEmbeddingConfig struct {
	Style        string `yaml:"style"`
	Endpoint     string `yaml:"endpoint"`
	ModelName    string `yaml:"model_name"`
	Name         string `yaml:"name"`
	APIKey       string `yaml:"api_key"`
	APIKeyLegacy string `yaml:"apiKey"`
	DefaultDims  int    `yaml:"default_dims"`
}

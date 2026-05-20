package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLoadDiscoversUserLevelConfig(t *testing.T) {
	t.Parallel()

	projectRoot := t.TempDir()
	userHome := t.TempDir()
	writeConfigFile(t, userHome, ".speecdex/config.yaml", `
config:
  ignored_entries:
    - .git
    - ignored.md
  service:
    port: 9001
  indexing:
    chunk_size: 1500
    chunk_overlap: 300
`)

	got := loadForTest(t, projectRoot, userHome)
	assertStrings(t, got.IgnoredEntries, []string{".git", "ignored.md"})
	if got.Service.Port != 9001 {
		t.Fatalf("Service.Port = %d, want 9001", got.Service.Port)
	}
	if got.Indexing.ChunkSize != 1500 {
		t.Fatalf("ChunkSize = %d, want 1500", got.Indexing.ChunkSize)
	}
	if got.Indexing.ChunkOverlap != 300 {
		t.Fatalf("ChunkOverlap = %d, want 300", got.Indexing.ChunkOverlap)
	}
}

func TestLoadDiscoversProjectLevelConfig(t *testing.T) {
	t.Parallel()

	projectRoot := t.TempDir()
	userHome := t.TempDir()
	writeConfigFile(t, projectRoot, ".speecdex/config.yaml", `
config:
  service:
    port: 9100
`)

	got := loadForTest(t, projectRoot, userHome)
	if got.Service.Port != 9100 {
		t.Fatalf("Service.Port = %d, want 9100", got.Service.Port)
	}
}

func TestLoadSupportsYMLFiles(t *testing.T) {
	t.Parallel()

	projectRoot := t.TempDir()
	userHome := t.TempDir()
	writeConfigFile(t, projectRoot, ".speecdex/config.yml", `
config:
  service:
    port: 9101
`)
	writeConfigFile(t, projectRoot, ".speecdex/llms.yml", `
llms:
  embedding:
    style: openai-compatible
    endpoint: http://localhost:1234/v1
    model_name: yml-model
    default_dims: 768
`)

	got := loadForTest(t, projectRoot, userHome)
	if got.Service.Port != 9101 {
		t.Fatalf("Service.Port = %d, want 9101", got.Service.Port)
	}
	if got.Embedding == nil || got.Embedding.ModelName != "yml-model" {
		t.Fatalf("Embedding = %#v, want model_name yml-model", got.Embedding)
	}
}

func TestLoadPrefersYAMLOverYMLInSameScope(t *testing.T) {
	t.Parallel()

	projectRoot := t.TempDir()
	userHome := t.TempDir()
	writeConfigFile(t, projectRoot, ".speecdex/config.yml", `
config:
  service:
    port: 9102
`)
	writeConfigFile(t, projectRoot, ".speecdex/config.yaml", `
config:
  service:
    port: 9103
`)

	got := loadForTest(t, projectRoot, userHome)
	if got.Service.Port != 9103 {
		t.Fatalf("Service.Port = %d, want yaml file port 9103", got.Service.Port)
	}
}

func TestLoadProjectConfigOverridesUserConfigFieldByField(t *testing.T) {
	t.Parallel()

	projectRoot := t.TempDir()
	userHome := t.TempDir()
	writeConfigFile(t, userHome, ".speecdex/config.yaml", `
config:
  ignored_entries:
    - user.md
  service:
    port: 9002
  indexing:
    chunk_size: 1600
    chunk_overlap: 250
`)
	writeConfigFile(t, userHome, ".speecdex/llms.yaml", `
llms:
  embedding:
    style: openai-compatible
    endpoint: http://user.example/v1
    model_name: user-model
    api_key: user-secret
    default_dims: 512
`)
	writeConfigFile(t, projectRoot, ".speecdex/config.yaml", `
config:
  indexing:
    chunk_overlap: 350
`)
	writeConfigFile(t, projectRoot, ".speecdex/llms.yaml", `
llms:
  embedding:
    model_name: project-model
`)

	got := loadForTest(t, projectRoot, userHome)
	if got.Service.Port != 9002 {
		t.Fatalf("Service.Port = %d, want inherited user port 9002", got.Service.Port)
	}
	if got.Indexing.ChunkSize != 1600 {
		t.Fatalf("ChunkSize = %d, want inherited user size 1600", got.Indexing.ChunkSize)
	}
	if got.Indexing.ChunkOverlap != 350 {
		t.Fatalf("ChunkOverlap = %d, want project override 350", got.Indexing.ChunkOverlap)
	}
	if got.Embedding == nil {
		t.Fatal("Embedding is nil")
	}
	if got.Embedding.Endpoint != "http://user.example/v1" {
		t.Fatalf("Embedding.Endpoint = %q, want inherited user endpoint", got.Embedding.Endpoint)
	}
	if got.Embedding.ModelName != "project-model" {
		t.Fatalf("Embedding.ModelName = %q, want project-model", got.Embedding.ModelName)
	}
	if got.Embedding.APIKey != "user-secret" {
		t.Fatalf("Embedding.APIKey = %q, want inherited user secret", got.Embedding.APIKey)
	}
}

func TestLoadParsesOpenAICompatibleEmbeddingConfig(t *testing.T) {
	t.Parallel()

	projectRoot := t.TempDir()
	userHome := t.TempDir()
	writeConfigFile(t, projectRoot, ".speecdex/llms.yaml", `
llms:
  embedding:
    style: openai-compatible
    endpoint: http://localhost:1234/v1
    model_name: text-embedding-model
    api_key: sk-test
    default_dims: 768
`)

	got := loadForTest(t, projectRoot, userHome)
	want := &ModelConfig{
		Style:       StyleOpenAICompatible,
		Endpoint:    "http://localhost:1234/v1",
		ModelName:   "text-embedding-model",
		APIKey:      "sk-test",
		DefaultDims: 768,
	}
	if !reflect.DeepEqual(got.Embedding, want) {
		t.Fatalf("Embedding = %#v, want %#v", got.Embedding, want)
	}
}

func TestLoadParsesGGUFEmbeddingConfig(t *testing.T) {
	t.Parallel()

	projectRoot := t.TempDir()
	userHome := t.TempDir()
	writeConfigFile(t, projectRoot, ".speecdex/llms.yaml", `
llms:
  embedding:
    style: gguf
    model_name: /models/bge-small.gguf
    default_dims: 384
`)

	got := loadForTest(t, projectRoot, userHome)
	want := &ModelConfig{
		Style:       StyleGGUF,
		ModelName:   "/models/bge-small.gguf",
		DefaultDims: 384,
	}
	if !reflect.DeepEqual(got.Embedding, want) {
		t.Fatalf("Embedding = %#v, want %#v", got.Embedding, want)
	}
}

func TestLoadParsesRerankingAndLegacyAliases(t *testing.T) {
	t.Parallel()

	projectRoot := t.TempDir()
	userHome := t.TempDir()
	writeConfigFile(t, projectRoot, ".speecdex/llms.yaml", `
llms:
  embedding:
    style: openai-compatible
    endpoint: http://localhost:1234/v1
    name: alias-embedding-model
    apiKey: sk-legacy-embedding
    default_dims: 768
  reranking:
    style: openai-compatible
    endpoint: http://localhost:1234/v1
    name: alias-reranker
    apiKey: sk-legacy-reranking
`)

	got := loadForTest(t, projectRoot, userHome)
	if got.Embedding == nil {
		t.Fatal("Embedding is nil")
	}
	if got.Embedding.ModelName != "alias-embedding-model" {
		t.Fatalf("Embedding.ModelName = %q, want alias-embedding-model", got.Embedding.ModelName)
	}
	if got.Embedding.APIKey != "sk-legacy-embedding" {
		t.Fatalf("Embedding.APIKey = %q, want legacy key", got.Embedding.APIKey)
	}
	if got.Reranking == nil {
		t.Fatal("Reranking is nil")
	}
	if got.Reranking.ModelName != "alias-reranker" {
		t.Fatalf("Reranking.ModelName = %q, want alias-reranker", got.Reranking.ModelName)
	}
	if got.Reranking.APIKey != "sk-legacy-reranking" {
		t.Fatalf("Reranking.APIKey = %q, want legacy reranking key", got.Reranking.APIKey)
	}
}

func TestLoadUsesDefaultsWithoutFiles(t *testing.T) {
	t.Parallel()

	got := loadForTest(t, t.TempDir(), t.TempDir())
	if got.Service.Port != DefaultServicePort {
		t.Fatalf("Service.Port = %d, want %d", got.Service.Port, DefaultServicePort)
	}
	if got.Indexing.ChunkSize != DefaultChunkSize {
		t.Fatalf("ChunkSize = %d, want %d", got.Indexing.ChunkSize, DefaultChunkSize)
	}
	if got.Indexing.ChunkOverlap != DefaultChunkOverlap {
		t.Fatalf("ChunkOverlap = %d, want %d", got.Indexing.ChunkOverlap, DefaultChunkOverlap)
	}
	if got.Embedding != nil {
		t.Fatalf("Embedding = %#v, want nil", got.Embedding)
	}
}

func TestLoadValidationErrorIncludesPathFieldAndRedactsKeys(t *testing.T) {
	t.Parallel()

	projectRoot := t.TempDir()
	userHome := t.TempDir()
	invalidPath := writeConfigFile(t, projectRoot, ".speecdex/config.yaml", `
config:
  service:
    port: 70000
`)
	writeConfigFile(t, projectRoot, ".speecdex/llms.yaml", `
llms:
  embedding:
    style: openai-compatible
    endpoint: http://localhost:1234/v1
    model_name: text-embedding-model
    apiKey: sk-should-not-leak
    default_dims: 768
`)

	_, err := Load(LoadOptions{ProjectRoot: projectRoot, UserHome: userHome})
	if err == nil {
		t.Fatal("Load() error = nil, want validation error")
	}
	message := err.Error()
	if !strings.Contains(message, invalidPath) {
		t.Fatalf("error = %q, want path %q", message, invalidPath)
	}
	if !strings.Contains(message, "config.service.port") {
		t.Fatalf("error = %q, want field config.service.port", message)
	}
	if strings.Contains(message, "sk-should-not-leak") {
		t.Fatalf("error leaked API key: %q", message)
	}
}

func TestLoadRejectsChunkOverlapGreaterThanOrEqualToSize(t *testing.T) {
	t.Parallel()

	projectRoot := t.TempDir()
	userHome := t.TempDir()
	writeConfigFile(t, projectRoot, ".speecdex/config.yaml", `
config:
  indexing:
    chunk_size: 100
    chunk_overlap: 100
`)

	_, err := Load(LoadOptions{ProjectRoot: projectRoot, UserHome: userHome})
	if err == nil {
		t.Fatal("Load() error = nil, want validation error")
	}
	if !strings.Contains(err.Error(), "config.indexing.chunk_overlap") {
		t.Fatalf("error = %q, want chunk_overlap field", err.Error())
	}
}

func TestLoadRejectsIncompleteModelConfig(t *testing.T) {
	t.Parallel()

	projectRoot := t.TempDir()
	userHome := t.TempDir()
	path := writeConfigFile(t, projectRoot, ".speecdex/llms.yaml", `
llms:
  embedding:
    style: openai-compatible
    model_name: text-embedding-model
    default_dims: 768
`)

	_, err := Load(LoadOptions{ProjectRoot: projectRoot, UserHome: userHome})
	if err == nil {
		t.Fatal("Load() error = nil, want validation error")
	}
	message := err.Error()
	if !strings.Contains(message, path) {
		t.Fatalf("error = %q, want path %q", message, path)
	}
	if !strings.Contains(message, "llms.embedding.endpoint") {
		t.Fatalf("error = %q, want llms.embedding.endpoint field", message)
	}
}

func TestRedactSecrets(t *testing.T) {
	t.Parallel()

	got := RedactSecrets(`apiKey: "sk-secret" api_key=sk-other Authorization: Bearer sk-third https://user:token@example.com/v1`)
	for _, secret := range []string{"sk-secret", "sk-other", "sk-third", "user:token"} {
		if strings.Contains(got, secret) {
			t.Fatalf("RedactSecrets() = %q, leaked %q", got, secret)
		}
	}
	if strings.Count(got, "<redacted>") != 4 {
		t.Fatalf("RedactSecrets() = %q, want four redactions", got)
	}
}

func loadForTest(t *testing.T, projectRoot string, userHome string) Config {
	t.Helper()

	cfg, err := Load(LoadOptions{ProjectRoot: projectRoot, UserHome: userHome})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	return cfg
}

func writeConfigFile(t *testing.T, root string, name string, contents string) string {
	t.Helper()

	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte(strings.TrimSpace(contents)+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return path
}

func assertStrings(t *testing.T, got []string, want []string) {
	t.Helper()

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("strings = %#v, want %#v", got, want)
	}
}

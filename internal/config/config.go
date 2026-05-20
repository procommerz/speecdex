package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	DefaultServicePort  = 8248
	DefaultChunkSize    = 1200
	DefaultChunkOverlap = 200

	StyleOpenAICompatible = "openai-compatible"
	StyleGGUF             = "gguf"
)

type LoadOptions struct {
	ProjectRoot string
	UserHome    string
}

type Config struct {
	IgnoredEntries []string
	Service        ServiceConfig
	Indexing       IndexingConfig
	Embedding      *ModelConfig
	Reranking      *ModelConfig
}

type ServiceConfig struct {
	Port int
}

type IndexingConfig struct {
	ChunkSize    int
	ChunkOverlap int
}

type ModelConfig struct {
	Style       string
	Endpoint    string
	ModelName   string
	APIKey      string
	DefaultDims int
}

type ConfigError struct {
	Path    string
	Field   string
	Message string
	Err     error
}

func (e *ConfigError) Error() string {
	var b strings.Builder
	b.WriteString("configuration error")
	if e.Path != "" {
		fmt.Fprintf(&b, " in %s", e.Path)
	}
	if e.Field != "" {
		fmt.Fprintf(&b, " field %s", e.Field)
	}
	if e.Message != "" {
		fmt.Fprintf(&b, ": %s", e.Message)
	}
	if e.Err != nil {
		fmt.Fprintf(&b, ": %v", e.Err)
	}
	return RedactSecrets(b.String())
}

func Load(opts LoadOptions) (Config, error) {
	if opts.ProjectRoot == "" {
		wd, err := os.Getwd()
		if err != nil {
			return Config{}, &ConfigError{Message: "determine project root", Err: err}
		}
		opts.ProjectRoot = wd
	}
	if opts.UserHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return Config{}, &ConfigError{Message: "determine user home", Err: err}
		}
		opts.UserHome = home
	}

	state := newState()
	for _, scope := range []string{
		filepath.Join(opts.UserHome, ".speecdex"),
		filepath.Join(opts.ProjectRoot, ".speecdex"),
	} {
		if err := state.loadAppConfig(pickConfigPath(scope, "config")); err != nil {
			return Config{}, err
		}
		if err := state.loadLLMConfig(pickConfigPath(scope, "llms")); err != nil {
			return Config{}, err
		}
	}

	cfg := state.config
	if err := state.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func RedactSecrets(message string) string {
	patterns := []struct {
		pattern     *regexp.Regexp
		replacement string
	}{
		{
			pattern:     regexp.MustCompile(`(?i)(api[_-]?key\s*[:=]\s*)("[^"]*"|'[^']*'|[^\s,}]+)`),
			replacement: `${1}<redacted>`,
		},
		{
			pattern:     regexp.MustCompile(`(?i)(authorization\s*[:=]\s*bearer\s+)([^\s,}]+)`),
			replacement: `${1}<redacted>`,
		},
		{
			pattern:     regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://)([^/@\s]+)@`),
			replacement: `${1}<redacted>@`,
		},
	}

	redacted := message
	for _, replacement := range patterns {
		redacted = replacement.pattern.ReplaceAllString(redacted, replacement.replacement)
	}
	return redacted
}

type state struct {
	config Config
	source sourceMap
}

type sourceMap struct {
	servicePort  string
	chunkSize    string
	chunkOverlap string
	embedding    modelSource
	reranking    modelSource
}

type modelSource struct {
	model    string
	style    string
	endpoint string
	name     string
	key      string
	dims     string
}

func newState() *state {
	return &state{
		config: Config{
			Service:  ServiceConfig{Port: DefaultServicePort},
			Indexing: IndexingConfig{ChunkSize: DefaultChunkSize, ChunkOverlap: DefaultChunkOverlap},
		},
	}
}

func (s *state) loadAppConfig(path string) error {
	if path == "" {
		return nil
	}

	var doc appDocument
	if err := decodeYAMLFile(path, &doc); err != nil {
		return err
	}

	if doc.Config.IgnoredEntries != nil {
		s.config.IgnoredEntries = append([]string(nil), (*doc.Config.IgnoredEntries)...)
	}
	if doc.Config.Service.Port != nil {
		s.config.Service.Port = *doc.Config.Service.Port
		s.source.servicePort = path
	}
	if doc.Config.Indexing.ChunkSize != nil {
		s.config.Indexing.ChunkSize = *doc.Config.Indexing.ChunkSize
		s.source.chunkSize = path
	}
	if doc.Config.Indexing.ChunkOverlap != nil {
		s.config.Indexing.ChunkOverlap = *doc.Config.Indexing.ChunkOverlap
		s.source.chunkOverlap = path
	}
	return nil
}

func (s *state) loadLLMConfig(path string) error {
	if path == "" {
		return nil
	}

	var doc llmDocument
	if err := decodeYAMLFile(path, &doc); err != nil {
		return err
	}

	if doc.LLMs.Embedding != nil {
		if s.config.Embedding == nil {
			s.config.Embedding = &ModelConfig{}
		}
		applyModel(s.config.Embedding, doc.LLMs.Embedding, path, &s.source.embedding)
	}
	if doc.LLMs.Reranking != nil {
		if s.config.Reranking == nil {
			s.config.Reranking = &ModelConfig{}
		}
		applyModel(s.config.Reranking, doc.LLMs.Reranking, path, &s.source.reranking)
	}
	return nil
}

func (s *state) validate() error {
	if s.config.Service.Port < 1 || s.config.Service.Port > 65535 {
		return &ConfigError{
			Path:    firstNonEmpty(s.source.servicePort),
			Field:   "config.service.port",
			Message: "must be between 1 and 65535",
		}
	}
	if s.config.Indexing.ChunkOverlap >= s.config.Indexing.ChunkSize {
		return &ConfigError{
			Path:    firstNonEmpty(s.source.chunkOverlap, s.source.chunkSize),
			Field:   "config.indexing.chunk_overlap",
			Message: "must be less than config.indexing.chunk_size",
		}
	}
	if s.config.Embedding != nil {
		if err := validateModel(*s.config.Embedding, s.source.embedding, "llms.embedding", true); err != nil {
			return err
		}
	}
	if s.config.Reranking != nil {
		if err := validateModel(*s.config.Reranking, s.source.reranking, "llms.reranking", false); err != nil {
			return err
		}
	}
	return nil
}

func validateModel(model ModelConfig, source modelSource, prefix string, requireDims bool) error {
	switch model.Style {
	case StyleOpenAICompatible:
		if strings.TrimSpace(model.Endpoint) == "" {
			return missingField(source.model, prefix+".endpoint")
		}
		if strings.TrimSpace(model.ModelName) == "" {
			return missingField(source.model, prefix+".model_name")
		}
		if requireDims && model.DefaultDims <= 0 {
			return missingField(source.model, prefix+".default_dims")
		}
	case StyleGGUF:
		if strings.TrimSpace(model.ModelName) == "" {
			return missingField(source.model, prefix+".model_name")
		}
		if requireDims && model.DefaultDims <= 0 {
			return missingField(source.model, prefix+".default_dims")
		}
	case "":
		return missingField(source.model, prefix+".style")
	default:
		return &ConfigError{
			Path:    source.style,
			Field:   prefix + ".style",
			Message: fmt.Sprintf("unsupported style %q", model.Style),
		}
	}
	return nil
}

func missingField(path string, field string) error {
	return &ConfigError{
		Path:    path,
		Field:   field,
		Message: "is required",
	}
}

func pickConfigPath(scopeDir string, basename string) string {
	yamlPath := filepath.Join(scopeDir, basename+".yaml")
	if fileExists(yamlPath) {
		return yamlPath
	}

	ymlPath := filepath.Join(scopeDir, basename+".yml")
	if fileExists(ymlPath) {
		return ymlPath
	}

	return ""
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func decodeYAMLFile(path string, out any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return &ConfigError{Path: path, Message: "read file", Err: err}
	}
	if err := yaml.Unmarshal(data, out); err != nil {
		return &ConfigError{Path: path, Message: "parse YAML", Err: err}
	}
	return nil
}

func applyModel(target *ModelConfig, raw *rawModel, path string, source *modelSource) {
	source.model = path
	if raw.Style != nil {
		target.Style = *raw.Style
		source.style = path
	}
	if raw.Endpoint != nil {
		target.Endpoint = *raw.Endpoint
		source.endpoint = path
	}
	if raw.ModelName != nil {
		target.ModelName = *raw.ModelName
		source.name = path
	}
	if raw.Name != nil {
		target.ModelName = *raw.Name
		source.name = path
	}
	if raw.APIKey != nil {
		target.APIKey = *raw.APIKey
		source.key = path
	}
	if raw.APIKeyLegacy != nil {
		target.APIKey = *raw.APIKeyLegacy
		source.key = path
	}
	if raw.DefaultDims != nil {
		target.DefaultDims = *raw.DefaultDims
		source.dims = path
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

type appDocument struct {
	Config rawAppConfig `yaml:"config"`
}

type rawAppConfig struct {
	IgnoredEntries *[]string         `yaml:"ignored_entries"`
	Service        rawServiceConfig  `yaml:"service"`
	Indexing       rawIndexingConfig `yaml:"indexing"`
}

type rawServiceConfig struct {
	Port *int `yaml:"port"`
}

type rawIndexingConfig struct {
	ChunkSize    *int `yaml:"chunk_size"`
	ChunkOverlap *int `yaml:"chunk_overlap"`
}

type llmDocument struct {
	LLMs rawLLMConfig `yaml:"llms"`
}

type rawLLMConfig struct {
	Embedding *rawModel `yaml:"embedding"`
	Reranking *rawModel `yaml:"reranking"`
}

type rawModel struct {
	Style        *string `yaml:"style"`
	Endpoint     *string `yaml:"endpoint"`
	ModelName    *string `yaml:"model_name"`
	Name         *string `yaml:"name"`
	APIKey       *string `yaml:"api_key"`
	APIKeyLegacy *string `yaml:"apiKey"`
	DefaultDims  *int    `yaml:"default_dims"`
}

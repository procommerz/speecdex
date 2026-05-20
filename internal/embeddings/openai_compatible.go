package embeddings

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

	"github.com/procommerz/speecdex-search/internal/config"
)

type OpenAICompatibleOptions struct {
	Endpoint   string
	ModelName  string
	APIKey     string
	Dimensions int
	HTTPClient *http.Client
}

type OpenAICompatibleClient struct {
	endpoint   string
	modelName  string
	apiKey     string
	dimensions int
	httpClient *http.Client
}

func NewOpenAICompatibleClient(opts OpenAICompatibleOptions) (*OpenAICompatibleClient, error) {
	endpoint, err := normalizeEmbeddingsEndpoint(opts.Endpoint)
	if err != nil {
		return nil, redactError(err)
	}

	modelName := strings.TrimSpace(opts.ModelName)
	if modelName == "" {
		return nil, newError("openai-compatible embedding model name is required")
	}

	if opts.Dimensions <= 0 {
		return nil, newError("openai-compatible embedding dimensions must be greater than zero")
	}

	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	return &OpenAICompatibleClient{
		endpoint:   endpoint,
		modelName:  modelName,
		apiKey:     opts.APIKey,
		dimensions: opts.Dimensions,
		httpClient: httpClient,
	}, nil
}

func (c *OpenAICompatibleClient) Embed(ctx context.Context, inputs []string) ([][]float64, error) {
	if len(inputs) == 0 {
		return [][]float64{}, nil
	}

	body, err := json.Marshal(embeddingRequest{
		Model: c.modelName,
		Input: inputs,
	})
	if err != nil {
		return nil, redactError(fmt.Errorf("encode embedding request: %w", err))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, redactError(fmt.Errorf("create embedding request: %w", err))
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, redactError(fmt.Errorf("send embedding request: %w", err))
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, redactError(fmt.Errorf("read embedding response: %w", err))
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, newError("embedding request failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var parsed embeddingResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, redactError(fmt.Errorf("decode embedding response: %w", err))
	}

	vectors, err := c.orderedVectors(parsed.Data, len(inputs))
	if err != nil {
		return nil, err
	}
	return vectors, nil
}

func (c *OpenAICompatibleClient) orderedVectors(data []embeddingData, inputCount int) ([][]float64, error) {
	if len(data) != inputCount {
		return nil, newError("embedding response returned %d vectors for %d inputs", len(data), inputCount)
	}

	vectors := make([][]float64, inputCount)
	seen := make([]bool, inputCount)
	for _, item := range data {
		if item.Index < 0 || item.Index >= inputCount {
			return nil, newError("embedding response index %d is out of range for %d inputs", item.Index, inputCount)
		}
		if seen[item.Index] {
			return nil, newError("embedding response contains duplicate index %d", item.Index)
		}
		if len(item.Embedding) != c.dimensions {
			return nil, newError("embedding response index %d has %d dimensions, want %d", item.Index, len(item.Embedding), c.dimensions)
		}

		seen[item.Index] = true
		vectors[item.Index] = item.Embedding
	}

	for index, ok := range seen {
		if !ok {
			return nil, newError("embedding response is missing vector for index %d", index)
		}
	}

	return vectors, nil
}

func normalizeEmbeddingsEndpoint(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", errors.New("openai-compatible embedding endpoint is required")
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("parse openai-compatible embedding endpoint: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("openai-compatible embedding endpoint must use http or https")
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("openai-compatible embedding endpoint must include a host")
	}

	parsed.User = nil
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	if !strings.HasSuffix(parsed.Path, "/embeddings") {
		parsed.Path += "/embeddings"
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

func newError(format string, args ...any) error {
	return errors.New(config.RedactSecrets(fmt.Sprintf(format, args...)))
}

func redactError(err error) error {
	if err == nil {
		return nil
	}
	return redactedError{err: err}
}

type redactedError struct {
	err error
}

func (e redactedError) Error() string {
	return config.RedactSecrets(e.err.Error())
}

func (e redactedError) Unwrap() error {
	return e.err
}

type embeddingRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embeddingResponse struct {
	Data  []embeddingData `json:"data"`
	Model string          `json:"model"`
}

type embeddingData struct {
	Index     int       `json:"index"`
	Embedding []float64 `json:"embedding"`
}

package embeddings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

func TestOpenAICompatibleClientSendsRequestAndPreservesResponseOrder(t *testing.T) {
	t.Parallel()

	requests := make(chan requestCapture, 1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var gotRequest embeddingRequest
		if err := json.NewDecoder(r.Body).Decode(&gotRequest); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		requests <- requestCapture{
			Path:        r.URL.Path,
			Auth:        r.Header.Get("Authorization"),
			ContentType: r.Header.Get("Content-Type"),
			Body:        gotRequest,
		}

		writeJSON(t, w, embeddingResponse{
			Data: []embeddingData{
				{Index: 2, Embedding: []float64{3, 3, 3}},
				{Index: 0, Embedding: []float64{1, 1, 1}},
				{Index: 1, Embedding: []float64{2, 2, 2}},
			},
			Model: "test-model",
		})
	}))
	defer server.Close()

	client, err := NewOpenAICompatibleClient(OpenAICompatibleOptions{
		Endpoint:   server.URL + "/v1/",
		ModelName:  "test-model",
		APIKey:     "sk-test-secret",
		Dimensions: 3,
	})
	if err != nil {
		t.Fatalf("NewOpenAICompatibleClient() error = %v", err)
	}

	got, err := client.Embed(context.Background(), []string{"first", "second", "third"})
	if err != nil {
		t.Fatalf("Embed() error = %v", err)
	}

	gotRequest := <-requests
	if gotRequest.Path != "/v1/embeddings" {
		t.Fatalf("request path = %q, want /v1/embeddings", gotRequest.Path)
	}
	if gotRequest.Auth != "Bearer sk-test-secret" {
		t.Fatalf("Authorization = %q, want bearer token", gotRequest.Auth)
	}
	if gotRequest.ContentType != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", gotRequest.ContentType)
	}
	wantBody := embeddingRequest{
		Model: "test-model",
		Input: []string{"first", "second", "third"},
	}
	if !reflect.DeepEqual(gotRequest.Body, wantBody) {
		t.Fatalf("request = %#v, want %#v", gotRequest.Body, wantBody)
	}

	want := [][]float64{
		{1, 1, 1},
		{2, 2, 2},
		{3, 3, 3},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("vectors = %#v, want %#v", got, want)
	}
}

func TestOpenAICompatibleClientOmitsAuthWhenAPIKeyIsEmpty(t *testing.T) {
	t.Parallel()

	authHeaders := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeaders <- r.Header.Get("Authorization")
		writeJSON(t, w, embeddingResponse{
			Data: []embeddingData{{Index: 0, Embedding: []float64{1, 2}}},
		})
	}))
	defer server.Close()

	client, err := NewOpenAICompatibleClient(OpenAICompatibleOptions{
		Endpoint:   server.URL + "/v1",
		ModelName:  "test-model",
		Dimensions: 2,
	})
	if err != nil {
		t.Fatalf("NewOpenAICompatibleClient() error = %v", err)
	}

	if _, err := client.Embed(context.Background(), []string{"first"}); err != nil {
		t.Fatalf("Embed() error = %v", err)
	}
	gotAuth := <-authHeaders
	if gotAuth != "" {
		t.Fatalf("Authorization = %q, want empty", gotAuth)
	}
}

func TestOpenAICompatibleClientReturnsEmptyVectorsWithoutRequestForEmptyInput(t *testing.T) {
	t.Parallel()

	var requests int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client, err := NewOpenAICompatibleClient(OpenAICompatibleOptions{
		Endpoint:   server.URL + "/v1",
		ModelName:  "test-model",
		Dimensions: 2,
	})
	if err != nil {
		t.Fatalf("NewOpenAICompatibleClient() error = %v", err)
	}

	got, err := client.Embed(context.Background(), nil)
	if err != nil {
		t.Fatalf("Embed() error = %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("vectors length = %d, want 0", len(got))
	}
	if requests != 0 {
		t.Fatalf("requests = %d, want 0", requests)
	}
}

func TestOpenAICompatibleClientRejectsInvalidResponses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		inputs     []string
		statusCode int
		body       string
		wantError  string
		notWant    string
	}{
		{
			name: "dimension mismatch",
			inputs: []string{
				"first",
			},
			statusCode: http.StatusOK,
			body:       `{"data":[{"index":0,"embedding":[1]}]}`,
			wantError:  "has 1 dimensions, want 2",
		},
		{
			name: "non 2xx response redacts authorization",
			inputs: []string{
				"first",
			},
			statusCode: http.StatusUnauthorized,
			body:       `Authorization: Bearer sk-test-secret`,
			wantError:  "embedding request failed with status 401",
			notWant:    "sk-test-secret",
		},
		{
			name: "malformed json",
			inputs: []string{
				"first",
			},
			statusCode: http.StatusOK,
			body:       `{not json`,
			wantError:  "decode embedding response",
		},
		{
			name: "missing vector",
			inputs: []string{
				"first",
				"second",
			},
			statusCode: http.StatusOK,
			body:       `{"data":[{"index":0,"embedding":[1,2]}]}`,
			wantError:  "returned 1 vectors for 2 inputs",
		},
		{
			name: "duplicate index",
			inputs: []string{
				"first",
				"second",
			},
			statusCode: http.StatusOK,
			body:       `{"data":[{"index":0,"embedding":[1,2]},{"index":0,"embedding":[3,4]}]}`,
			wantError:  "duplicate index 0",
		},
		{
			name: "negative index",
			inputs: []string{
				"first",
			},
			statusCode: http.StatusOK,
			body:       `{"data":[{"index":-1,"embedding":[1,2]}]}`,
			wantError:  "index -1 is out of range",
		},
		{
			name: "out of range index",
			inputs: []string{
				"first",
			},
			statusCode: http.StatusOK,
			body:       `{"data":[{"index":1,"embedding":[1,2]}]}`,
			wantError:  "index 1 is out of range",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				fmt.Fprint(w, tt.body)
			}))
			defer server.Close()

			client, err := NewOpenAICompatibleClient(OpenAICompatibleOptions{
				Endpoint:   server.URL + "/v1",
				ModelName:  "test-model",
				APIKey:     "sk-test-secret",
				Dimensions: 2,
			})
			if err != nil {
				t.Fatalf("NewOpenAICompatibleClient() error = %v", err)
			}

			_, err = client.Embed(context.Background(), tt.inputs)
			if err == nil {
				t.Fatal("Embed() error = nil, want error")
			}
			if !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("Embed() error = %q, want to contain %q", err.Error(), tt.wantError)
			}
			if tt.notWant != "" && strings.Contains(err.Error(), tt.notWant) {
				t.Fatalf("Embed() error leaked secret: %q", err.Error())
			}
		})
	}
}

func TestNewOpenAICompatibleClientRejectsInvalidOptions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		opts      OpenAICompatibleOptions
		wantError string
	}{
		{
			name: "missing endpoint",
			opts: OpenAICompatibleOptions{
				ModelName:  "test-model",
				Dimensions: 2,
			},
			wantError: "endpoint is required",
		},
		{
			name: "missing model",
			opts: OpenAICompatibleOptions{
				Endpoint:   "http://localhost:1234/v1",
				Dimensions: 2,
			},
			wantError: "model name is required",
		},
		{
			name: "invalid dimensions",
			opts: OpenAICompatibleOptions{
				Endpoint:   "http://localhost:1234/v1",
				ModelName:  "test-model",
				Dimensions: 0,
			},
			wantError: "dimensions must be greater than zero",
		},
		{
			name: "invalid scheme",
			opts: OpenAICompatibleOptions{
				Endpoint:   "ftp://localhost/v1",
				ModelName:  "test-model",
				Dimensions: 2,
			},
			wantError: "must use http or https",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := NewOpenAICompatibleClient(tt.opts)
			if err == nil {
				t.Fatal("NewOpenAICompatibleClient() error = nil, want error")
			}
			if !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("NewOpenAICompatibleClient() error = %q, want to contain %q", err.Error(), tt.wantError)
			}
		})
	}
}

func TestNormalizeEmbeddingsEndpointStripsURLUserinfo(t *testing.T) {
	t.Parallel()

	got, err := normalizeEmbeddingsEndpoint("https://user:secret@example.com/v1")
	if err != nil {
		t.Fatalf("normalizeEmbeddingsEndpoint() error = %v", err)
	}
	want := "https://example.com/v1/embeddings"
	if got != want {
		t.Fatalf("endpoint = %q, want %q", got, want)
	}
}

func TestNewOpenAICompatibleClientRedactsEndpointUserinfoInErrors(t *testing.T) {
	t.Parallel()

	_, err := NewOpenAICompatibleClient(OpenAICompatibleOptions{
		Endpoint:   "https://user:secret@example.com/%zz",
		ModelName:  "test-model",
		Dimensions: 2,
	})
	if err == nil {
		t.Fatal("NewOpenAICompatibleClient() error = nil, want parse error")
	}
	if strings.Contains(err.Error(), "user:secret") || strings.Contains(err.Error(), "secret@example.com") {
		t.Fatalf("NewOpenAICompatibleClient() error leaked URL credentials: %q", err.Error())
	}
	if !strings.Contains(err.Error(), "https://<redacted>@example.com/%zz") {
		t.Fatalf("NewOpenAICompatibleClient() error = %q, want redacted endpoint userinfo", err.Error())
	}
}

func TestRedactErrorPreservesErrorChain(t *testing.T) {
	t.Parallel()

	err := redactError(fmt.Errorf("send embedding request to https://user:secret@example.com/v1: %w", context.Canceled))
	if err == nil {
		t.Fatal("redactError() error = nil, want wrapped error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("errors.Is(err, context.Canceled) = false for %v", err)
	}
	if strings.Contains(err.Error(), "user:secret") {
		t.Fatalf("redactError() leaked URL credentials: %q", err.Error())
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
}

type requestCapture struct {
	Path        string
	Auth        string
	ContentType string
	Body        embeddingRequest
}

package cli

import (
	"bytes"
	"encoding/gob"
	"encoding/json"
	"github.com/procommerz/speecdex-search/internal/config"
	"github.com/procommerz/speecdex-search/internal/storage"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func runForTest(t *testing.T, args []string, stdout *bytes.Buffer, stderr *bytes.Buffer, projectRoot string, userHome string) int {
	t.Helper()

	return run(args, stdout, stderr, config.LoadOptions{ProjectRoot: projectRoot, UserHome: userHome})
}

func runForTestWithClock(t *testing.T, args []string, stdout *bytes.Buffer, stderr *bytes.Buffer, projectRoot string, userHome string, now func() time.Time) int {
	t.Helper()

	return runWithClock(args, stdout, stderr, config.LoadOptions{ProjectRoot: projectRoot, UserHome: userHome}, now)
}

func steppedClock(start time.Time, step time.Duration) func() time.Time {
	current := start.Add(-step)
	return func() time.Time {
		current = current.Add(step)
		return current
	}
}

func assertProgressLine(t *testing.T, got string, pattern string) {
	t.Helper()

	if !regexp.MustCompile(pattern).MatchString(got) {
		t.Fatalf("stderr = %q, want progress line matching %q", got, pattern)
	}
}

func stubGitBranch(branch string) func() {
	previous := detectGitBranch
	detectGitBranch = func(string) string {
		return branch
	}
	return func() {
		detectGitBranch = previous
	}
}

func writeTestConfig(t *testing.T, root string, name string, contents string) string {
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

func writeProjectFile(t *testing.T, root string, name string, contents string) {
	t.Helper()

	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

func writeEmbeddingConfig(t *testing.T, root string, endpoint string, modelName string, dimensions int) {
	t.Helper()

	writeTestConfig(t, root, ".speecdex/llms.yaml", `
llms:
  embedding:
    style: openai-compatible
    endpoint: `+endpoint+`
    model_name: `+modelName+`
    default_dims: `+strconv.Itoa(dimensions)+`
`)
}

func writeSearchIndex(t *testing.T, root string, chunks []storage.Chunk, modelName string, dimensions int, gitBranch ...string) {
	t.Helper()

	branch := ""
	if len(gitBranch) > 0 {
		branch = gitBranch[0]
	}
	idx := storage.Index{
		Header: storage.Header{
			FormatName:             storage.FormatName,
			FormatVersion:          storage.FormatVersion,
			CreatedAt:              time.Date(2026, 5, 20, 8, 0, 0, 0, time.UTC),
			GitBranch:              branch,
			EmbeddingProviderStyle: "openai-compatible",
			EmbeddingModelIdentity: modelName,
			EmbeddingDimensions:    dimensions,
			DistanceMetric:         storage.DistanceMetricCosine,
		},
		Sources: []storage.SourceFile{{
			RelativePath: "docs/example.md",
			ContentHash:  "hash",
			Size:         128,
			ModifiedAt:   time.Date(2026, 5, 20, 7, 30, 0, 0, time.UTC),
		}},
		Chunks: chunks,
	}
	if err := storage.Write(root, idx); err != nil {
		t.Fatalf("storage.Write() error = %v", err)
	}
}

func writeUnsupportedVersionIndex(t *testing.T, root string) {
	t.Helper()

	idx := storage.Index{
		Header: storage.Header{
			FormatName:             storage.FormatName,
			FormatVersion:          1,
			CreatedAt:              time.Date(2026, 5, 20, 8, 0, 0, 0, time.UTC),
			EmbeddingProviderStyle: "openai-compatible",
			EmbeddingModelIdentity: "test-model",
			EmbeddingDimensions:    2,
			DistanceMetric:         storage.DistanceMetricCosine,
			ChunkSize:              200,
			ChunkOverlap:           20,
		},
	}
	path := storage.ArtifactPath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(index dir) error = %v", err)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("Create(index) error = %v", err)
	}
	defer file.Close()
	if err := gob.NewEncoder(file).Encode(idx); err != nil {
		t.Fatalf("Encode(index) error = %v", err)
	}
}

type embeddingRecorder struct {
	mu       sync.Mutex
	requests []embeddingRequestCapture
	next     float64
}

func newEmbeddingRecorder(t *testing.T) (*httptest.Server, *embeddingRecorder) {
	t.Helper()

	recorder := &embeddingRecorder{next: 1}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var got embeddingRequestCapture
		got.Path = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&got.Body); err != nil {
			t.Errorf("Decode() error = %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		recorder.mu.Lock()
		recorder.requests = append(recorder.requests, got)
		data := make([]map[string]any, len(got.Body.Input))
		for i := range got.Body.Input {
			value := recorder.next
			recorder.next++
			data[i] = map[string]any{
				"index":     i,
				"embedding": []float64{value, value + 0.5},
			}
		}
		recorder.mu.Unlock()

		writeJSON(t, w, map[string]any{
			"data":  data,
			"model": "test-model",
		})
	}))
	return server, recorder
}

func (r *embeddingRecorder) ResetRequests() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requests = nil
}

func (r *embeddingRecorder) RequestCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.requests)
}

func (r *embeddingRecorder) Requests() []embeddingRequestCapture {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]embeddingRequestCapture, len(r.requests))
	copy(out, r.requests)
	return out
}

func chunksByPath(chunks []storage.Chunk) map[string]storage.Chunk {
	out := make(map[string]storage.Chunk, len(chunks))
	for _, chunk := range chunks {
		if !chunk.Deleted {
			out[chunk.SourcePath] = chunk
		}
	}
	return out
}

func writeJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
}

type embeddingRequestCapture struct {
	Path string
	Body struct {
		Model string   `json:"model"`
		Input []string `json:"input"`
	}
}

package cli

import (
	"bytes"
	"encoding/json"
	"github.com/procommerz/speecdex-search/internal/storage"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestRunSemanticSearchUsesExistingIndexAndWritesFencedYAML(t *testing.T) {
	t.Parallel()

	projectRoot := t.TempDir()
	userHome := t.TempDir()
	requests := make(chan embeddingRequestCapture, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var got embeddingRequestCapture
		got.Path = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&got.Body); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		requests <- got
		writeJSON(t, w, map[string]any{
			"data": []map[string]any{{
				"index":     0,
				"embedding": []float64{1, 0},
			}},
			"model": "test-model",
		})
	}))
	defer server.Close()

	writeEmbeddingConfig(t, projectRoot, server.URL+"/v1", "test-model", 2)
	writeSearchIndex(t, projectRoot, []storage.Chunk{
		{ID: "chunk-a", SourcePath: "docs/a.md", StartLine: 1, EndLine: 3, Text: "Root business object", Vector: []float64{1, 0}},
		{ID: "chunk-b", SourcePath: "docs/b.md", StartLine: 5, EndLine: 7, Text: "Other text", Vector: []float64{0, 1}},
	}, "test-model", 2, "feature/docs")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := runForTest(t, []string{"--query", "root business object"}, &stdout, &stderr, projectRoot, userHome)
	if code != ExitOK {
		t.Fatalf("Run() exit code = %d, want %d; stderr = %q", code, ExitOK, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("Run() wrote unexpected stderr: %q", stderr.String())
	}
	gotRequest := <-requests
	if gotRequest.Path != "/v1/embeddings" || gotRequest.Body.Model != "test-model" || len(gotRequest.Body.Input) != 1 {
		t.Fatalf("embedding request = %#v, want /v1/embeddings with model and query", gotRequest)
	}
	got := stdout.String()
	for _, want := range []string{"```yaml\n", "results:", "file: docs/a.md", "start_line: 1", "- semantic", "text: Root business object", "indexed_branch: feature/docs", "```\n"} {
		if !strings.Contains(got, want) {
			t.Fatalf("stdout = %q, want to contain %q", got, want)
		}
	}
}

func TestRunLiteralOnlySearchDoesNotRequireEmbeddingConfig(t *testing.T) {
	t.Parallel()

	projectRoot := t.TempDir()
	userHome := t.TempDir()
	writeSearchIndex(t, projectRoot, []storage.Chunk{
		{ID: "chunk-a", SourcePath: "docs/a.md", StartLine: 1, EndLine: 3, Text: "extends BusinessObject", Vector: []float64{1, 0}},
		{ID: "chunk-b", SourcePath: "docs/b.md", StartLine: 5, EndLine: 7, Text: "implements BusinessObject", Vector: []float64{0, 1}},
	}, "test-model", 2)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := runForTest(t, []string{"--text", "extends BusinessObject", "--text", "missing"}, &stdout, &stderr, projectRoot, userHome)
	if code != ExitOK {
		t.Fatalf("Run() exit code = %d, want %d; stderr = %q", code, ExitOK, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("Run() wrote unexpected stderr: %q", stderr.String())
	}
	got := stdout.String()
	for _, want := range []string{"```yaml\n", "file: docs/a.md", "- text", "matched_text:", "- extends BusinessObject"} {
		if !strings.Contains(got, want) {
			t.Fatalf("stdout = %q, want to contain %q", got, want)
		}
	}
	if strings.Contains(got, "docs/b.md") {
		t.Fatalf("stdout = %q, did not expect unmatched chunk", got)
	}
}

func TestRunCombinedSearchUsesORSemantics(t *testing.T) {
	t.Parallel()

	projectRoot := t.TempDir()
	userHome := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]any{
			"data": []map[string]any{{"index": 0, "embedding": []float64{1, 0}}},
		})
	}))
	defer server.Close()

	writeEmbeddingConfig(t, projectRoot, server.URL+"/v1", "test-model", 2)
	writeSearchIndex(t, projectRoot, []storage.Chunk{
		{ID: "chunk-a", SourcePath: "docs/a.md", StartLine: 1, EndLine: 3, Text: "semantic and extends BusinessObject", Vector: []float64{1, 0}},
		{ID: "chunk-b", SourcePath: "docs/b.md", StartLine: 5, EndLine: 7, Text: "literal only implements BusinessObject", Vector: []float64{-1, 0}},
	}, "test-model", 2)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := runForTest(t, []string{"--query", "root", "--text", "extends BusinessObject", "--text", "implements BusinessObject"}, &stdout, &stderr, projectRoot, userHome)
	if code != ExitOK {
		t.Fatalf("Run() exit code = %d, want %d; stderr = %q", code, ExitOK, stderr.String())
	}
	got := stdout.String()
	for _, want := range []string{"file: docs/a.md", "file: docs/b.md", "- semantic", "- text", "- implements BusinessObject"} {
		if !strings.Contains(got, want) {
			t.Fatalf("stdout = %q, want to contain %q", got, want)
		}
	}
	if strings.Count(got, "file: docs/a.md") != 1 {
		t.Fatalf("stdout = %q, want docs/a.md deduplicated", got)
	}
}

func TestRunShowBranchPrintsStoredBranch(t *testing.T) {
	t.Parallel()

	projectRoot := t.TempDir()
	userHome := t.TempDir()
	writeSearchIndex(t, projectRoot, []storage.Chunk{
		{ID: "chunk-a", SourcePath: "docs/a.md", StartLine: 1, EndLine: 3, Text: "Root business object", Vector: []float64{1, 0}},
	}, "test-model", 2, "feature/docs")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := runForTest(t, []string{"--show-branch"}, &stdout, &stderr, projectRoot, userHome)
	if code != ExitOK {
		t.Fatalf("Run() exit code = %d, want %d; stderr = %q", code, ExitOK, stderr.String())
	}
	if stdout.String() != "feature/docs\n" {
		t.Fatalf("stdout = %q, want stored branch", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty stderr", stderr.String())
	}
}

func TestRunShowBranchPrintsNothingForEmptyBranch(t *testing.T) {
	t.Parallel()

	projectRoot := t.TempDir()
	userHome := t.TempDir()
	writeSearchIndex(t, projectRoot, []storage.Chunk{
		{ID: "chunk-a", SourcePath: "docs/a.md", StartLine: 1, EndLine: 3, Text: "Root business object", Vector: []float64{1, 0}},
	}, "test-model", 2)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := runForTest(t, []string{"--show-branch"}, &stdout, &stderr, projectRoot, userHome)
	if code != ExitOK {
		t.Fatalf("Run() exit code = %d, want %d; stderr = %q", code, ExitOK, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want no output for empty branch", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty stderr", stderr.String())
	}
}

func TestRunShowBranchMissingIndexExitsRuntimeErrorWithoutRebuilding(t *testing.T) {
	t.Parallel()

	projectRoot := t.TempDir()
	userHome := t.TempDir()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := runForTest(t, []string{"--show-branch"}, &stdout, &stderr, projectRoot, userHome)
	if code != ExitRuntimeError {
		t.Fatalf("Run() exit code = %d, want %d", code, ExitRuntimeError)
	}
	if stdout.Len() != 0 {
		t.Fatalf("Run() wrote unexpected stdout: %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "index artifact is missing") {
		t.Fatalf("stderr = %q, want missing index error", stderr.String())
	}
	if _, err := os.Stat(storage.ArtifactPath(projectRoot)); !os.IsNotExist(err) {
		t.Fatalf("index artifact stat error = %v, want missing artifact", err)
	}
}

func TestRunMissingIndexExitsRuntimeErrorWithoutRebuilding(t *testing.T) {
	t.Parallel()

	projectRoot := t.TempDir()
	userHome := t.TempDir()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := runForTest(t, []string{"--text", "anything"}, &stdout, &stderr, projectRoot, userHome)
	if code != ExitRuntimeError {
		t.Fatalf("Run() exit code = %d, want %d", code, ExitRuntimeError)
	}
	if stdout.Len() != 0 {
		t.Fatalf("Run() wrote unexpected stdout: %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "index artifact is missing") {
		t.Fatalf("stderr = %q, want missing index error", stderr.String())
	}
	if _, err := os.Stat(storage.ArtifactPath(projectRoot)); !os.IsNotExist(err) {
		t.Fatalf("index artifact stat error = %v, want missing artifact", err)
	}
}

func TestRunIncompatibleIndexExitsRuntimeError(t *testing.T) {
	t.Parallel()

	projectRoot := t.TempDir()
	userHome := t.TempDir()
	writeEmbeddingConfig(t, projectRoot, "http://127.0.0.1:1/v1", "other-model", 2)
	writeSearchIndex(t, projectRoot, []storage.Chunk{
		{ID: "chunk-a", SourcePath: "docs/a.md", StartLine: 1, EndLine: 3, Text: "Root business object", Vector: []float64{1, 0}},
	}, "test-model", 2)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := runForTest(t, []string{"--query", "root"}, &stdout, &stderr, projectRoot, userHome)
	if code != ExitRuntimeError {
		t.Fatalf("Run() exit code = %d, want %d", code, ExitRuntimeError)
	}
	if stdout.Len() != 0 {
		t.Fatalf("Run() wrote unexpected stdout: %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "incompatible with active model") {
		t.Fatalf("stderr = %q, want incompatible model error", stderr.String())
	}
}

func TestRunReturnsRuntimeErrorForInvalidConfig(t *testing.T) {
	t.Parallel()

	projectRoot := t.TempDir()
	userHome := t.TempDir()
	writeTestConfig(t, projectRoot, ".speecdex/config.yaml", `
config:
  service:
    port: 70000
`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := runForTest(t, nil, &stdout, &stderr, projectRoot, userHome)
	if code != ExitRuntimeError {
		t.Fatalf("Run() exit code = %d, want %d", code, ExitRuntimeError)
	}
	if stdout.Len() != 0 {
		t.Fatalf("Run() wrote unexpected stdout: %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "config.service.port") {
		t.Fatalf("Run() stderr = %q, want config.service.port", stderr.String())
	}
}

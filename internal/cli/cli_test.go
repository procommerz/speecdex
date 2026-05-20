package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/procommerz/speecdex-search/internal/config"
	"github.com/procommerz/speecdex-search/internal/storage"
)

func TestParseSelectsModes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want Options
	}{
		{
			name: "no flags selects indexing mode",
			args: nil,
			want: Options{Mode: ModeIndex},
		},
		{
			name: "query selects search mode",
			args: []string{"--query", "root"},
			want: Options{Mode: ModeSearch, Query: "root"},
		},
		{
			name: "repeated text preserves all values",
			args: []string{"--text", "extends BusinessObject", "--text", "implements BusinessObject"},
			want: Options{Mode: ModeSearch, Text: []string{"extends BusinessObject", "implements BusinessObject"}},
		},
		{
			name: "query and text select combined search mode",
			args: []string{"--query", "root", "--text", "extends BusinessObject"},
			want: Options{Mode: ModeSearch, Query: "root", Text: []string{"extends BusinessObject"}},
		},
		{
			name: "service selects service mode",
			args: []string{"--service"},
			want: Options{Mode: ModeService, Service: true},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var stderr bytes.Buffer
			got, err := Parse(tt.args, &stderr)
			if err != nil {
				t.Fatalf("Parse() error = %v, stderr = %q", err, stderr.String())
			}

			assertOptions(t, got, tt.want)
			if stderr.Len() != 0 {
				t.Fatalf("Parse() wrote unexpected stderr: %q", stderr.String())
			}
		})
	}
}

func TestRunRejectsInvalidUsageWithExitCodeTwo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		args       []string
		wantStderr string
	}{
		{
			name:       "service rejects query",
			args:       []string{"--service", "--query", "root"},
			wantStderr: "--service cannot be combined",
		},
		{
			name:       "empty query is invalid",
			args:       []string{"--query", "   "},
			wantStderr: "--query requires a non-empty value",
		},
		{
			name:       "empty text is invalid",
			args:       []string{"--text", ""},
			wantStderr: "--text requires a non-empty value",
		},
		{
			name:       "unknown flag is invalid",
			args:       []string{"--unknown"},
			wantStderr: "Usage:",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var stdout bytes.Buffer
			var stderr bytes.Buffer
			code := Run(tt.args, &stdout, &stderr)
			if code != ExitUsageError {
				t.Fatalf("Run() exit code = %d, want %d", code, ExitUsageError)
			}
			if stdout.Len() != 0 {
				t.Fatalf("Run() wrote unexpected stdout: %q", stdout.String())
			}
			if !strings.Contains(stderr.String(), tt.wantStderr) {
				t.Fatalf("Run() stderr = %q, want to contain %q", stderr.String(), tt.wantStderr)
			}
		})
	}
}

func TestRunReturnsRuntimeErrorForUnimplementedServiceMode(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := runForTest(t, []string{"--service"}, &stdout, &stderr, t.TempDir(), t.TempDir())
	if code != ExitRuntimeError {
		t.Fatalf("Run() exit code = %d, want %d", code, ExitRuntimeError)
	}
	if stdout.Len() != 0 {
		t.Fatalf("Run() wrote unexpected stdout: %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "speecdex service mode is not implemented yet") {
		t.Fatalf("Run() stderr = %q, want service unimplemented error", stderr.String())
	}
}

func TestRunIndexesMarkdownProjectAndWritesSummary(t *testing.T) {
	t.Parallel()

	projectRoot := t.TempDir()
	userHome := t.TempDir()
	requests := make(chan embeddingRequestCapture, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var got embeddingRequestCapture
		got.Path = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&got.Body); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		requests <- got

		data := make([]map[string]any, len(got.Body.Input))
		for i := range got.Body.Input {
			data[i] = map[string]any{
				"index":     i,
				"embedding": []float64{float64(i + 1), float64(i + 2)},
			}
		}
		writeJSON(t, w, map[string]any{
			"data":  data,
			"model": "test-model",
		})
	}))
	defer server.Close()

	writeEmbeddingConfig(t, projectRoot, server.URL+"/v1", "test-model", 2)
	writeTestConfig(t, projectRoot, ".speecdex/config.yaml", `
config:
  ignored_entries:
    - docs/private
  indexing:
    chunk_size: 200
    chunk_overlap: 20
`)
	writeProjectFile(t, projectRoot, "docs/a.md", "# Alpha\nRoot business object\n")
	writeProjectFile(t, projectRoot, "docs/b.markdown", "# Bravo\nimplements BusinessObject\n")
	writeProjectFile(t, projectRoot, "docs/private/secret.md", "# Secret\nshould not embed\n")
	writeProjectFile(t, projectRoot, "notes.txt", "# Not markdown\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := runForTestWithClock(t, nil, &stdout, &stderr, projectRoot, userHome, steppedClock(
		time.Date(2026, 5, 20, 8, 0, 0, 0, time.UTC),
		time.Second,
	))
	if code != ExitOK {
		t.Fatalf("Run() exit code = %d, want %d; stderr = %q", code, ExitOK, stderr.String())
	}

	gotRequests := []embeddingRequestCapture{<-requests, <-requests}
	for _, gotRequest := range gotRequests {
		if gotRequest.Path != "/v1/embeddings" || gotRequest.Body.Model != "test-model" {
			t.Fatalf("embedding request = %#v, want /v1/embeddings with test model", gotRequest)
		}
	}
	if len(gotRequests[0].Body.Input) != 1 || len(gotRequests[1].Body.Input) != 1 {
		t.Fatalf("embedding input counts = %d, %d; want one request per file: %#v", len(gotRequests[0].Body.Input), len(gotRequests[1].Body.Input), gotRequests)
	}
	allInputs := append(append([]string(nil), gotRequests[0].Body.Input...), gotRequests[1].Body.Input...)
	if strings.Contains(strings.Join(allInputs, "\n"), "should not embed") {
		t.Fatalf("embedding inputs include ignored file text: %#v", allInputs)
	}

	idx, err := storage.Read(projectRoot, storage.ReadOptions{})
	if err != nil {
		t.Fatalf("storage.Read() error = %v", err)
	}
	if idx.Header.EmbeddingProviderStyle != "openai-compatible" ||
		idx.Header.EmbeddingModelIdentity != "test-model" ||
		idx.Header.EmbeddingDimensions != 2 ||
		idx.Header.ChunkSize != 200 ||
		idx.Header.ChunkOverlap != 20 {
		t.Fatalf("index header = %#v, want configured embedding and chunk metadata", idx.Header)
	}
	if !reflect.DeepEqual(idx.Header.IgnoredEntries, []string{"docs/private"}) {
		t.Fatalf("IgnoredEntries = %#v, want docs/private", idx.Header.IgnoredEntries)
	}
	if len(idx.Sources) != 2 || idx.Sources[0].RelativePath != "docs/a.md" || idx.Sources[1].RelativePath != "docs/b.markdown" {
		t.Fatalf("Sources = %#v, want indexed markdown sources in lexical order", idx.Sources)
	}
	if len(idx.Chunks) != 2 || idx.Chunks[0].Text != allInputs[0] || idx.Chunks[1].Text != allInputs[1] {
		t.Fatalf("Chunks = %#v, want chunks matching embedded inputs %#v", idx.Chunks, allInputs)
	}
	if !reflect.DeepEqual(idx.Chunks[0].Vector, []float64{1, 2}) || !reflect.DeepEqual(idx.Chunks[1].Vector, []float64{1, 2}) {
		t.Fatalf("chunk vectors = %#v, %#v; want fake embedding vectors", idx.Chunks[0].Vector, idx.Chunks[1].Vector)
	}

	gotStderr := stderr.String()
	assertProgressLine(t, gotStderr, `Indexing file 1/2: docs/a\.md \(1 chunks\) \| elapsed 1s \| 1\.00 chunks/s \| ETA 1s`)
	assertProgressLine(t, gotStderr, `Indexing file 2/2: docs/b\.markdown \(1 chunks\) \| elapsed 2s \| 1\.00 chunks/s \| ETA 0s`)

	gotStdout := stdout.String()
	for _, want := range []string{
		"Project root: " + projectRoot,
		"Markdown files discovered: 2",
		"Markdown files indexed: 2",
		"Chunks indexed: 2",
		"Ignored entries: 1",
		"Embedding provider: openai-compatible/test-model",
		"Index artifact: " + storage.ArtifactPath(projectRoot),
	} {
		if !strings.Contains(gotStdout, want) {
			t.Fatalf("stdout = %q, want to contain %q", gotStdout, want)
		}
	}
}

func TestRunIndexAppliesOnlyEntriesBeforeIgnoredEntries(t *testing.T) {
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

		data := make([]map[string]any, len(got.Body.Input))
		for i := range got.Body.Input {
			data[i] = map[string]any{
				"index":     i,
				"embedding": []float64{float64(i + 1), float64(i + 2)},
			}
		}
		writeJSON(t, w, map[string]any{
			"data":  data,
			"model": "test-model",
		})
	}))
	defer server.Close()

	writeEmbeddingConfig(t, projectRoot, server.URL+"/v1", "test-model", 2)
	writeTestConfig(t, projectRoot, ".speecdex/config.yaml", `
config:
  only_entries:
    - docs
  ignored_entries:
    - docs/private
  indexing:
    chunk_size: 200
    chunk_overlap: 20
`)
	writeProjectFile(t, projectRoot, ".speecdex/included-never.md", "# Artifact\nshould not embed\n")
	writeProjectFile(t, projectRoot, "docs/allowed.md", "# Allowed\nshould embed\n")
	writeProjectFile(t, projectRoot, "docs/private/secret.md", "# Secret\nshould not embed\n")
	writeProjectFile(t, projectRoot, "outside.md", "# Outside\nshould not embed\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := runForTest(t, nil, &stdout, &stderr, projectRoot, userHome)
	if code != ExitOK {
		t.Fatalf("Run() exit code = %d, want %d; stderr = %q", code, ExitOK, stderr.String())
	}
	if !strings.Contains(stderr.String(), "Indexing file 1/1: docs/allowed.md (1 chunks)") {
		t.Fatalf("Run() stderr = %q, want indexing progress for included file", stderr.String())
	}

	gotRequest := <-requests
	if len(gotRequest.Body.Input) != 1 || !strings.Contains(gotRequest.Body.Input[0], "should embed") {
		t.Fatalf("embedding inputs = %#v, want only included non-ignored markdown", gotRequest.Body.Input)
	}
	joinedInput := strings.Join(gotRequest.Body.Input, "\n")
	for _, unexpected := range []string{"Secret", "Outside", "Artifact"} {
		if strings.Contains(joinedInput, unexpected) {
			t.Fatalf("embedding inputs = %#v, did not expect %q", gotRequest.Body.Input, unexpected)
		}
	}

	idx, err := storage.Read(projectRoot, storage.ReadOptions{})
	if err != nil {
		t.Fatalf("storage.Read() error = %v", err)
	}
	if len(idx.Sources) != 1 || idx.Sources[0].RelativePath != "docs/allowed.md" {
		t.Fatalf("Sources = %#v, want only docs/allowed.md", idx.Sources)
	}
	if !reflect.DeepEqual(idx.Header.IgnoredEntries, []string{"docs/private"}) {
		t.Fatalf("IgnoredEntries = %#v, want docs/private", idx.Header.IgnoredEntries)
	}
	if strings.Contains(stdout.String(), "Only entries") {
		t.Fatalf("stdout = %q, did not expect only_entries summary", stdout.String())
	}
}

func TestRunIndexProgressUsesStableFileOrder(t *testing.T) {
	t.Parallel()

	projectRoot := t.TempDir()
	userHome := t.TempDir()
	requests := make(chan embeddingRequestCapture, 3)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var got embeddingRequestCapture
		got.Path = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&got.Body); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		requests <- got

		data := make([]map[string]any, len(got.Body.Input))
		for i := range got.Body.Input {
			data[i] = map[string]any{
				"index":     i,
				"embedding": []float64{float64(i + 1), float64(i + 2)},
			}
		}
		writeJSON(t, w, map[string]any{
			"data":  data,
			"model": "test-model",
		})
	}))
	defer server.Close()

	writeEmbeddingConfig(t, projectRoot, server.URL+"/v1", "test-model", 2)
	writeTestConfig(t, projectRoot, ".speecdex/config.yaml", `
config:
  indexing:
    chunk_size: 200
    chunk_overlap: 20
`)
	writeProjectFile(t, projectRoot, "z-last.md", "# Last\n")
	writeProjectFile(t, projectRoot, "a-first.md", "# First\n")
	writeProjectFile(t, projectRoot, "m-middle.md", "# Middle\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := runForTestWithClock(t, nil, &stdout, &stderr, projectRoot, userHome, steppedClock(
		time.Date(2026, 5, 20, 8, 0, 0, 0, time.UTC),
		time.Second,
	))
	if code != ExitOK {
		t.Fatalf("Run() exit code = %d, want %d; stderr = %q", code, ExitOK, stderr.String())
	}

	for range 3 {
		<-requests
	}

	got := stderr.String()
	first := strings.Index(got, "Indexing file 1/3: a-first.md (1 chunks)")
	middle := strings.Index(got, "Indexing file 2/3: m-middle.md (1 chunks)")
	last := strings.Index(got, "Indexing file 3/3: z-last.md (1 chunks)")
	if first < 0 || middle < 0 || last < 0 || !(first < middle && middle < last) {
		t.Fatalf("stderr = %q, want progress in stable lexical file order", got)
	}
}

func TestRunIndexRequiresEmbeddingConfig(t *testing.T) {
	t.Parallel()

	projectRoot := t.TempDir()
	userHome := t.TempDir()
	writeProjectFile(t, projectRoot, "docs/a.md", "# Alpha\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := runForTest(t, nil, &stdout, &stderr, projectRoot, userHome)
	if code != ExitRuntimeError {
		t.Fatalf("Run() exit code = %d, want %d", code, ExitRuntimeError)
	}
	if stdout.Len() != 0 {
		t.Fatalf("Run() wrote unexpected stdout: %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "indexing requires llms.embedding configuration") {
		t.Fatalf("stderr = %q, want missing embedding config error", stderr.String())
	}
	if _, err := os.Stat(storage.ArtifactPath(projectRoot)); !os.IsNotExist(err) {
		t.Fatalf("index artifact stat error = %v, want missing artifact", err)
	}
}

func TestRunIndexEmbeddingFailurePreservesPreviousIndex(t *testing.T) {
	t.Parallel()

	projectRoot := t.TempDir()
	userHome := t.TempDir()
	writeSearchIndex(t, projectRoot, []storage.Chunk{
		{ID: "old-chunk", SourcePath: "docs/old.md", StartLine: 1, EndLine: 1, Text: "previous index text", Vector: []float64{1, 0}},
	}, "old-model", 2)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("forced embedding failure"))
	}))
	defer server.Close()

	writeEmbeddingConfig(t, projectRoot, server.URL+"/v1", "test-model", 2)
	writeProjectFile(t, projectRoot, "docs/a.md", "# Alpha\nnew text\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := runForTest(t, nil, &stdout, &stderr, projectRoot, userHome)
	if code != ExitRuntimeError {
		t.Fatalf("Run() exit code = %d, want %d", code, ExitRuntimeError)
	}
	if stdout.Len() != 0 {
		t.Fatalf("Run() wrote unexpected stdout: %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "embed chunks") || !strings.Contains(stderr.String(), "forced embedding failure") {
		t.Fatalf("stderr = %q, want embedding failure diagnostic", stderr.String())
	}

	got, err := storage.Read(projectRoot, storage.ReadOptions{})
	if err != nil {
		t.Fatalf("storage.Read() error = %v", err)
	}
	if got.Header.EmbeddingModelIdentity != "old-model" || len(got.Chunks) != 1 || got.Chunks[0].Text != "previous index text" {
		t.Fatalf("index after failed rebuild = %#v, want previous index preserved", got)
	}
}

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
	}, "test-model", 2)

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
	for _, want := range []string{"```yaml\n", "results:", "file: docs/a.md", "start_line: 1", "- semantic", "text: Root business object", "```\n"} {
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

func assertOptions(t *testing.T, got Options, want Options) {
	t.Helper()

	if got.Mode != want.Mode {
		t.Fatalf("Mode = %q, want %q", got.Mode, want.Mode)
	}
	if got.Query != want.Query {
		t.Fatalf("Query = %q, want %q", got.Query, want.Query)
	}
	if got.Service != want.Service {
		t.Fatalf("Service = %t, want %t", got.Service, want.Service)
	}
	if len(got.Text) != len(want.Text) {
		t.Fatalf("Text length = %d, want %d; got %#v", len(got.Text), len(want.Text), got.Text)
	}
	for i := range got.Text {
		if got.Text[i] != want.Text[i] {
			t.Fatalf("Text[%d] = %q, want %q", i, got.Text[i], want.Text[i])
		}
	}
}

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

func writeSearchIndex(t *testing.T, root string, chunks []storage.Chunk, modelName string, dimensions int) {
	t.Helper()

	idx := storage.Index{
		Header: storage.Header{
			FormatName:             storage.FormatName,
			FormatVersion:          storage.FormatVersion,
			CreatedAt:              time.Date(2026, 5, 20, 8, 0, 0, 0, time.UTC),
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

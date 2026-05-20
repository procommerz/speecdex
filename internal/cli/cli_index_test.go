package cli

import (
	"bytes"
	"encoding/json"
	"github.com/procommerz/speecdex-search/internal/storage"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

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
	assertProgressLine(t, gotStderr, `Indexing file 1/2: docs/a\.md \(1 chunks, mean 28 chars\) \| elapsed 1s \| 1\.00 chunks/s \| ETA 1s`)
	assertProgressLine(t, gotStderr, `Indexing file 2/2: docs/b\.markdown \(1 chunks, mean 33 chars\) \| elapsed 2s \| 1\.00 chunks/s \| ETA 0s`)

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

func TestRunIndexStoresDetectedGitBranchAndWritesSummary(t *testing.T) {
	restore := stubGitBranch("feature/docs")
	defer restore()

	projectRoot := t.TempDir()
	userHome := t.TempDir()
	server, recorder := newEmbeddingRecorder(t)
	defer server.Close()

	writeEmbeddingConfig(t, projectRoot, server.URL+"/v1", "test-model", 2)
	writeTestConfig(t, projectRoot, ".speecdex/config.yaml", `
config:
  indexing:
    chunk_size: 200
    chunk_overlap: 20
`)
	writeProjectFile(t, projectRoot, "docs/a.md", "# Alpha\nRoot business object\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := runForTest(t, nil, &stdout, &stderr, projectRoot, userHome)
	if code != ExitOK {
		t.Fatalf("Run() exit code = %d, want %d; stderr = %q", code, ExitOK, stderr.String())
	}
	if got := recorder.RequestCount(); got != 1 {
		t.Fatalf("embedding request count = %d, want 1", got)
	}

	idx, err := storage.Read(projectRoot, storage.ReadOptions{})
	if err != nil {
		t.Fatalf("storage.Read() error = %v", err)
	}
	if idx.Header.GitBranch != "feature/docs" {
		t.Fatalf("GitBranch = %q, want feature/docs", idx.Header.GitBranch)
	}
	if !strings.Contains(stdout.String(), "Indexed git branch: feature/docs") {
		t.Fatalf("stdout = %q, want indexed git branch summary", stdout.String())
	}
}

func TestRunIndexTreatsMissingGitBranchAsEmpty(t *testing.T) {
	restore := stubGitBranch("")
	defer restore()

	projectRoot := t.TempDir()
	userHome := t.TempDir()
	server, _ := newEmbeddingRecorder(t)
	defer server.Close()

	writeEmbeddingConfig(t, projectRoot, server.URL+"/v1", "test-model", 2)
	writeTestConfig(t, projectRoot, ".speecdex/config.yaml", `
config:
  indexing:
    chunk_size: 200
    chunk_overlap: 20
`)
	writeProjectFile(t, projectRoot, "docs/a.md", "# Alpha\nRoot business object\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := runForTest(t, nil, &stdout, &stderr, projectRoot, userHome)
	if code != ExitOK {
		t.Fatalf("Run() exit code = %d, want %d; stderr = %q", code, ExitOK, stderr.String())
	}

	idx, err := storage.Read(projectRoot, storage.ReadOptions{})
	if err != nil {
		t.Fatalf("storage.Read() error = %v", err)
	}
	if idx.Header.GitBranch != "" {
		t.Fatalf("GitBranch = %q, want empty branch", idx.Header.GitBranch)
	}
	if strings.Contains(stdout.String(), "Indexed git branch:") {
		t.Fatalf("stdout = %q, did not expect indexed git branch summary", stdout.String())
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
	if !strings.Contains(stderr.String(), "Indexing file 1/1: docs/allowed.md (1 chunks, mean ") {
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
	first := strings.Index(got, "Indexing file 1/3: a-first.md (1 chunks, mean 7 chars)")
	middle := strings.Index(got, "Indexing file 2/3: m-middle.md (1 chunks, mean 8 chars)")
	last := strings.Index(got, "Indexing file 3/3: z-last.md (1 chunks, mean 6 chars)")
	if first < 0 || middle < 0 || last < 0 || !(first < middle && middle < last) {
		t.Fatalf("stderr = %q, want progress in stable lexical file order", got)
	}
}

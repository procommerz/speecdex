package cli

import (
	"bytes"
	"context"
	"github.com/procommerz/speecdex-search/internal/search"
	"github.com/procommerz/speecdex-search/internal/storage"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestRunIndexReusesUnchangedFilesByChecksum(t *testing.T) {
	t.Parallel()

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
	writeProjectFile(t, projectRoot, "docs/a.md", "# Alpha\nunchanged\n")
	writeProjectFile(t, projectRoot, "docs/b.md", "# Bravo\nunchanged\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := runForTest(t, nil, &stdout, &stderr, projectRoot, userHome)
	if code != ExitOK {
		t.Fatalf("first Run() exit code = %d, want %d; stderr = %q", code, ExitOK, stderr.String())
	}
	if got := recorder.RequestCount(); got != 2 {
		t.Fatalf("first embedding request count = %d, want 2", got)
	}
	first, err := storage.Read(projectRoot, storage.ReadOptions{})
	if err != nil {
		t.Fatalf("storage.Read(first) error = %v", err)
	}

	recorder.ResetRequests()
	stdout.Reset()
	stderr.Reset()
	code = runForTest(t, nil, &stdout, &stderr, projectRoot, userHome)
	if code != ExitOK {
		t.Fatalf("second Run() exit code = %d, want %d; stderr = %q", code, ExitOK, stderr.String())
	}
	if got := recorder.RequestCount(); got != 0 {
		t.Fatalf("second embedding request count = %d, want 0", got)
	}
	second, err := storage.Read(projectRoot, storage.ReadOptions{})
	if err != nil {
		t.Fatalf("storage.Read(second) error = %v", err)
	}
	if !reflect.DeepEqual(second.Chunks, first.Chunks) {
		t.Fatalf("second chunks = %#v, want reused first chunks %#v", second.Chunks, first.Chunks)
	}
	if !strings.Contains(stderr.String(), "Index rebuild: reused 2 chunks | embedded 0 chunks | retained deleted 0 chunks") {
		t.Fatalf("stderr = %q, want rebuild reuse diagnostic", stderr.String())
	}
	if !strings.Contains(stderr.String(), "Indexing file 1/2: docs/a.md (1 chunks, mean 17 chars)") ||
		!strings.Contains(stderr.String(), "Indexing file 2/2: docs/b.md (1 chunks, mean 17 chars)") {
		t.Fatalf("stderr = %q, want reused chunk progress with mean character lengths", stderr.String())
	}
	if strings.Contains(stderr.String(), "previous index cannot be reused") {
		t.Fatalf("stderr = %q, did not expect previous index warning for compatible reuse", stderr.String())
	}
}

func TestRunIndexEmbedsOnlyChangedFiles(t *testing.T) {
	t.Parallel()

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
	writeProjectFile(t, projectRoot, "docs/a.md", "# Alpha\nunchanged\n")
	writeProjectFile(t, projectRoot, "docs/b.md", "# Bravo\nold\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := runForTest(t, nil, &stdout, &stderr, projectRoot, userHome); code != ExitOK {
		t.Fatalf("first Run() exit code = %d, want %d; stderr = %q", code, ExitOK, stderr.String())
	}
	first, err := storage.Read(projectRoot, storage.ReadOptions{})
	if err != nil {
		t.Fatalf("storage.Read(first) error = %v", err)
	}
	firstByPath := chunksByPath(first.Chunks)

	writeProjectFile(t, projectRoot, "docs/b.md", "# Bravo\nchanged\n")
	recorder.ResetRequests()
	stdout.Reset()
	stderr.Reset()
	if code := runForTest(t, nil, &stdout, &stderr, projectRoot, userHome); code != ExitOK {
		t.Fatalf("second Run() exit code = %d, want %d; stderr = %q", code, ExitOK, stderr.String())
	}

	requests := recorder.Requests()
	if len(requests) != 1 || len(requests[0].Body.Input) != 1 {
		t.Fatalf("embedding requests = %#v, want one changed file chunk", requests)
	}
	if !strings.Contains(requests[0].Body.Input[0], "changed") || strings.Contains(requests[0].Body.Input[0], "Alpha") {
		t.Fatalf("embedding input = %#v, want only changed docs/b.md", requests[0].Body.Input)
	}
	second, err := storage.Read(projectRoot, storage.ReadOptions{})
	if err != nil {
		t.Fatalf("storage.Read(second) error = %v", err)
	}
	secondByPath := chunksByPath(second.Chunks)
	if !reflect.DeepEqual(secondByPath["docs/a.md"].Vector, firstByPath["docs/a.md"].Vector) {
		t.Fatalf("docs/a.md vector = %#v, want reused %#v", secondByPath["docs/a.md"].Vector, firstByPath["docs/a.md"].Vector)
	}
	if reflect.DeepEqual(secondByPath["docs/b.md"].Vector, firstByPath["docs/b.md"].Vector) {
		t.Fatalf("docs/b.md vector = %#v, want reembedded vector", secondByPath["docs/b.md"].Vector)
	}
	if !strings.Contains(stderr.String(), "Index rebuild: reused 1 chunks | embedded 1 chunks | retained deleted 0 chunks") {
		t.Fatalf("stderr = %q, want partial rebuild diagnostic", stderr.String())
	}
}

func TestRunIndexMarksDeletedFilesAndUndeletesRestoredSameChecksum(t *testing.T) {
	t.Parallel()

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
	contents := "# Alpha\nrestore me\n"
	writeProjectFile(t, projectRoot, "docs/a.md", contents)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := runForTest(t, nil, &stdout, &stderr, projectRoot, userHome); code != ExitOK {
		t.Fatalf("first Run() exit code = %d, want %d; stderr = %q", code, ExitOK, stderr.String())
	}
	first, err := storage.Read(projectRoot, storage.ReadOptions{})
	if err != nil {
		t.Fatalf("storage.Read(first) error = %v", err)
	}
	firstChunk := first.Chunks[0]

	if err := os.Remove(filepath.Join(projectRoot, "docs/a.md")); err != nil {
		t.Fatalf("Remove(docs/a.md) error = %v", err)
	}
	recorder.ResetRequests()
	stdout.Reset()
	stderr.Reset()
	if code := runForTest(t, nil, &stdout, &stderr, projectRoot, userHome); code != ExitOK {
		t.Fatalf("deleted Run() exit code = %d, want %d; stderr = %q", code, ExitOK, stderr.String())
	}
	if got := recorder.RequestCount(); got != 0 {
		t.Fatalf("deleted embedding request count = %d, want 0", got)
	}
	deleted, err := storage.Read(projectRoot, storage.ReadOptions{})
	if err != nil {
		t.Fatalf("storage.Read(deleted) error = %v", err)
	}
	if len(deleted.Sources) != 1 || !deleted.Sources[0].Deleted || len(deleted.Chunks) != 1 || !deleted.Chunks[0].Deleted {
		t.Fatalf("deleted index = %#v, want deleted source and chunk", deleted)
	}
	results, err := search.Run(context.Background(), deleted, search.Options{Text: []string{"restore me"}}, nil)
	if err != nil {
		t.Fatalf("search.Run(deleted) error = %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("deleted search results = %#v, want none", results)
	}

	writeProjectFile(t, projectRoot, "docs/a.md", contents)
	recorder.ResetRequests()
	stdout.Reset()
	stderr.Reset()
	if code := runForTest(t, nil, &stdout, &stderr, projectRoot, userHome); code != ExitOK {
		t.Fatalf("restored Run() exit code = %d, want %d; stderr = %q", code, ExitOK, stderr.String())
	}
	if got := recorder.RequestCount(); got != 0 {
		t.Fatalf("restored embedding request count = %d, want 0", got)
	}
	restored, err := storage.Read(projectRoot, storage.ReadOptions{})
	if err != nil {
		t.Fatalf("storage.Read(restored) error = %v", err)
	}
	if restored.Sources[0].Deleted || restored.Chunks[0].Deleted {
		t.Fatalf("restored index = %#v, want undeleted source and chunk", restored)
	}
	if restored.Chunks[0].ID != firstChunk.ID || !reflect.DeepEqual(restored.Chunks[0].Vector, firstChunk.Vector) {
		t.Fatalf("restored chunk = %#v, want reused chunk %#v", restored.Chunks[0], firstChunk)
	}
}

func TestRunIndexForceReembedsCurrentFiles(t *testing.T) {
	t.Parallel()

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
	writeProjectFile(t, projectRoot, "docs/a.md", "# Alpha\nsame text\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := runForTest(t, nil, &stdout, &stderr, projectRoot, userHome); code != ExitOK {
		t.Fatalf("first Run() exit code = %d, want %d; stderr = %q", code, ExitOK, stderr.String())
	}
	first, err := storage.Read(projectRoot, storage.ReadOptions{})
	if err != nil {
		t.Fatalf("storage.Read(first) error = %v", err)
	}

	recorder.ResetRequests()
	stdout.Reset()
	stderr.Reset()
	if code := runForTest(t, []string{"--force"}, &stdout, &stderr, projectRoot, userHome); code != ExitOK {
		t.Fatalf("force Run() exit code = %d, want %d; stderr = %q", code, ExitOK, stderr.String())
	}
	if got := recorder.RequestCount(); got != 1 {
		t.Fatalf("force embedding request count = %d, want 1", got)
	}
	forced, err := storage.Read(projectRoot, storage.ReadOptions{})
	if err != nil {
		t.Fatalf("storage.Read(forced) error = %v", err)
	}
	if forced.Chunks[0].ID != first.Chunks[0].ID {
		t.Fatalf("forced chunk ID = %q, want deterministic unchanged ID %q", forced.Chunks[0].ID, first.Chunks[0].ID)
	}
	if reflect.DeepEqual(forced.Chunks[0].Vector, first.Chunks[0].Vector) {
		t.Fatalf("forced vector = %#v, want reembedded vector different from %#v", forced.Chunks[0].Vector, first.Chunks[0].Vector)
	}
	if !strings.Contains(stderr.String(), "Index rebuild: reused 0 chunks | embedded 1 chunks | retained deleted 0 chunks") {
		t.Fatalf("stderr = %q, want force rebuild diagnostic", stderr.String())
	}
}

func TestRunIndexWarnsAndRebuildsWhenPreviousIndexVersionIsUnsupported(t *testing.T) {
	t.Parallel()

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
	writeProjectFile(t, projectRoot, "docs/a.md", "# Alpha\nfresh rebuild\n")
	writeUnsupportedVersionIndex(t, projectRoot)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := runForTest(t, nil, &stdout, &stderr, projectRoot, userHome)
	if code != ExitOK {
		t.Fatalf("Run() exit code = %d, want %d; stderr = %q", code, ExitOK, stderr.String())
	}
	if got := recorder.RequestCount(); got != 1 {
		t.Fatalf("embedding request count = %d, want full rebuild embedding request", got)
	}
	gotStderr := stderr.String()
	if !strings.Contains(gotStderr, "previous index cannot be reused") ||
		!strings.Contains(gotStderr, "unsupported index format version 1") {
		t.Fatalf("stderr = %q, want unsupported version reuse warning", gotStderr)
	}
	if !strings.Contains(gotStderr, "Index rebuild: reused 0 chunks | embedded 1 chunks | retained deleted 0 chunks") {
		t.Fatalf("stderr = %q, want full rebuild diagnostic", gotStderr)
	}
}

func TestRunIndexWarnsAndRebuildsWhenPreviousIndexConfigIsIncompatible(t *testing.T) {
	t.Parallel()

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
	writeProjectFile(t, projectRoot, "docs/a.md", "# Alpha\nsame content\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := runForTest(t, nil, &stdout, &stderr, projectRoot, userHome); code != ExitOK {
		t.Fatalf("first Run() exit code = %d, want %d; stderr = %q", code, ExitOK, stderr.String())
	}

	writeTestConfig(t, projectRoot, ".speecdex/config.yaml", `
config:
  indexing:
    chunk_size: 180
    chunk_overlap: 20
`)
	recorder.ResetRequests()
	stdout.Reset()
	stderr.Reset()
	code := runForTest(t, nil, &stdout, &stderr, projectRoot, userHome)
	if code != ExitOK {
		t.Fatalf("second Run() exit code = %d, want %d; stderr = %q", code, ExitOK, stderr.String())
	}
	if got := recorder.RequestCount(); got != 1 {
		t.Fatalf("embedding request count = %d, want full rebuild embedding request", got)
	}
	gotStderr := stderr.String()
	if !strings.Contains(gotStderr, "previous index cannot be reused: not compatible with active indexing configuration") {
		t.Fatalf("stderr = %q, want incompatible config reuse warning", gotStderr)
	}
	if !strings.Contains(gotStderr, "Index rebuild: reused 0 chunks | embedded 1 chunks | retained deleted 0 chunks") {
		t.Fatalf("stderr = %q, want full rebuild diagnostic", gotStderr)
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

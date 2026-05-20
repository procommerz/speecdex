package storage

import (
	"encoding/gob"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/procommerz/speecdex-search/internal/indexing"
)

func TestWriteCreatesIndexArtifactAndPreservesConfigFiles(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeStorageFile(t, root, ".speecdex/config.yaml", "config:\n  service:\n    port: 9000\n")
	writeStorageFile(t, root, ".speecdex/llms.yaml", "llms:\n  embedding:\n    model_name: keep\n")

	idx := sampleIndex(t, root)
	if err := Write(root, idx); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	if _, err := os.Stat(ArtifactPath(root)); err != nil {
		t.Fatalf("Stat(index.bin) error = %v", err)
	}
	assertFileContents(t, root, ".speecdex/config.yaml", "config:\n  service:\n    port: 9000\n")
	assertFileContents(t, root, ".speecdex/llms.yaml", "llms:\n  embedding:\n    model_name: keep\n")
}

func TestWriteCreatesSpeecdexDirectoryWhenMissing(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := Write(root, sampleIndex(t, root)); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, speecdexDataDirName)); err != nil {
		t.Fatalf("Stat(.speecdex) error = %v", err)
	}
}

func TestReadRoundTripsStoredIndexData(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	want := sampleIndex(t, root)
	if err := Write(root, want); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	got, err := Read(root, ReadOptions{})
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}

	if got.Header.FormatName != FormatName || got.Header.FormatVersion != FormatVersion {
		t.Fatalf("header format = %q/%d, want %q/%d", got.Header.FormatName, got.Header.FormatVersion, FormatName, FormatVersion)
	}
	if !reflect.DeepEqual(got.Sources, want.Sources) {
		t.Fatalf("Sources = %#v, want %#v", got.Sources, want.Sources)
	}
	if !reflect.DeepEqual(got.Chunks, want.Chunks) {
		t.Fatalf("Chunks = %#v, want %#v", got.Chunks, want.Chunks)
	}
}

func TestReadRejectsUnsupportedFormatVersionAndName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		mutate    func(*Index)
		wantError string
	}{
		{
			name: "format name",
			mutate: func(idx *Index) {
				idx.Header.FormatName = "other-index"
			},
			wantError: "unsupported index format",
		},
		{
			name: "format version",
			mutate: func(idx *Index) {
				idx.Header.FormatVersion = 999
			},
			wantError: "unsupported index format version 999",
		},
		{
			name: "distance metric",
			mutate: func(idx *Index) {
				idx.Header.DistanceMetric = "dot"
			},
			wantError: "unsupported index distance metric",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			idx := sampleIndex(t, root)
			tt.mutate(&idx)
			writeRawIndex(t, root, idx)

			_, err := Read(root, ReadOptions{})
			if err == nil {
				t.Fatal("Read() error = nil, want error")
			}
			if !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("Read() error = %q, want to contain %q", err.Error(), tt.wantError)
			}
		})
	}
}

func TestReadRejectsIncompatibleEmbeddingMetadata(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		opts      ReadOptions
		wantError string
	}{
		{
			name: "dimensions",
			opts: ReadOptions{Compatibility: CompatibilityOptions{
				EmbeddingDimensions: 2,
			}},
			wantError: "incompatible with active dimensions 2",
		},
		{
			name: "model identity",
			opts: ReadOptions{Compatibility: CompatibilityOptions{
				EmbeddingModelIdentity: "other-model",
			}},
			wantError: "incompatible with active model",
		},
		{
			name: "distance metric",
			opts: ReadOptions{Compatibility: CompatibilityOptions{
				DistanceMetric: "dot",
			}},
			wantError: "incompatible with active metric",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			if err := Write(root, sampleIndex(t, root)); err != nil {
				t.Fatalf("Write() error = %v", err)
			}

			_, err := Read(root, tt.opts)
			if err == nil {
				t.Fatal("Read() error = nil, want error")
			}
			if !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("Read() error = %q, want to contain %q", err.Error(), tt.wantError)
			}
		})
	}
}

func TestReadReturnsClearMissingIndexError(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	_, err := Read(root, ReadOptions{})
	if err == nil {
		t.Fatal("Read() error = nil, want missing artifact error")
	}
	if !strings.Contains(err.Error(), "index artifact is missing") || !strings.Contains(err.Error(), ".speecdex") {
		t.Fatalf("Read() error = %q, want missing index path", err.Error())
	}
}

func TestWriteFailurePreservesPreviousValidIndex(t *testing.T) {
	root := t.TempDir()
	previous := sampleIndex(t, root)
	if err := Write(root, previous); err != nil {
		t.Fatalf("initial Write() error = %v", err)
	}

	oldCreateTempFile := createTempFile
	createTempFile = func(string, string) (*os.File, error) {
		return nil, errors.New("forced temp failure")
	}
	defer func() {
		createTempFile = oldCreateTempFile
	}()

	next := sampleIndex(t, root)
	next.Chunks[0].Text = "new text that should not be persisted"
	if err := Write(root, next); err == nil {
		t.Fatal("Write() error = nil, want forced temp failure")
	}

	got, err := Read(root, ReadOptions{})
	if err != nil {
		t.Fatalf("Read() after failed Write() error = %v", err)
	}
	if got.Chunks[0].Text != previous.Chunks[0].Text {
		t.Fatalf("chunk text = %q, want preserved previous text %q", got.Chunks[0].Text, previous.Chunks[0].Text)
	}
}

func TestReadContainsSearchOutputDataWithoutSourceFiles(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	idx := sampleIndex(t, root)
	if err := Write(root, idx); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	got, err := Read(root, ReadOptions{Compatibility: CompatibilityOptions{
		EmbeddingModelIdentity: "test-model",
		EmbeddingDimensions:    3,
		DistanceMetric:         DistanceMetricCosine,
	}})
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}

	chunk := got.Chunks[0]
	if chunk.SourcePath == "" || chunk.StartLine == 0 || chunk.EndLine == 0 || chunk.Text == "" || len(chunk.Vector) != 3 {
		t.Fatalf("chunk lacks search data: %#v", chunk)
	}
}

func TestBuildIndexConvertsIndexingDataAndValidatesVectors(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	modifiedAt := time.Date(2026, 5, 20, 8, 0, 0, 0, time.UTC)
	files := []indexing.MarkdownFile{{
		RelativePath: "docs/example.md",
		ContentHash:  "hash",
		Size:         42,
		ModifiedAt:   modifiedAt,
	}}
	chunks := []indexing.Chunk{{
		ID:         "chunk-1",
		SourcePath: "docs/example.md",
		StartLine:  1,
		EndLine:    3,
		Text:       "chunk text",
	}}

	got, err := BuildIndex(BuildOptions{
		ProjectRoot:            root,
		EmbeddingProviderStyle: "openai-compatible",
		EmbeddingModelIdentity: "test-model",
		EmbeddingDimensions:    2,
		ChunkSize:              1200,
		ChunkOverlap:           200,
		IgnoredEntries:         []string{"z", "a"},
	}, files, chunks, [][]float64{{0.1, 0.2}})
	if err != nil {
		t.Fatalf("BuildIndex() error = %v", err)
	}

	if got.Header.DistanceMetric != DistanceMetricCosine {
		t.Fatalf("DistanceMetric = %q, want cosine", got.Header.DistanceMetric)
	}
	if !reflect.DeepEqual(got.Header.IgnoredEntries, []string{"a", "z"}) {
		t.Fatalf("IgnoredEntries = %#v, want sorted list", got.Header.IgnoredEntries)
	}
	if got.Sources[0].ModifiedAt != modifiedAt || got.Sources[0].ContentHash != "hash" {
		t.Fatalf("Source = %#v, want indexing metadata", got.Sources[0])
	}
	if got.Chunks[0].Text != "chunk text" || !reflect.DeepEqual(got.Chunks[0].Vector, []float64{0.1, 0.2}) {
		t.Fatalf("Chunk = %#v, want chunk text and vector", got.Chunks[0])
	}

	_, err = BuildIndex(BuildOptions{
		ProjectRoot:            root,
		EmbeddingModelIdentity: "test-model",
		EmbeddingDimensions:    3,
	}, files, chunks, [][]float64{{0.1, 0.2}})
	if err == nil {
		t.Fatal("BuildIndex() error = nil, want dimension mismatch")
	}
	if !strings.Contains(err.Error(), "has 2 dimensions, want 3") {
		t.Fatalf("BuildIndex() error = %q, want dimension mismatch", err.Error())
	}
}

func sampleIndex(t *testing.T, root string) Index {
	t.Helper()

	identity, err := projectRootIdentity(root)
	if err != nil {
		t.Fatalf("projectRootIdentity() error = %v", err)
	}
	return Index{
		Header: Header{
			FormatName:             FormatName,
			FormatVersion:          FormatVersion,
			CreatedAt:              time.Date(2026, 5, 20, 8, 0, 0, 0, time.UTC),
			ProjectRootIdentity:    identity,
			EmbeddingProviderStyle: "openai-compatible",
			EmbeddingModelIdentity: "test-model",
			EmbeddingDimensions:    3,
			DistanceMetric:         DistanceMetricCosine,
			ChunkSize:              1200,
			ChunkOverlap:           200,
			IgnoredEntries:         []string{".git"},
		},
		Sources: []SourceFile{{
			RelativePath: "docs/example.md",
			ContentHash:  "abc123",
			Size:         128,
			ModifiedAt:   time.Date(2026, 5, 20, 7, 30, 0, 0, time.UTC),
		}},
		Chunks: []Chunk{{
			ID:         "chunk-1",
			SourcePath: "docs/example.md",
			StartLine:  1,
			EndLine:    4,
			Text:       "# Example\n\nChunk text",
			Vector:     []float64{0.1, 0.2, 0.3},
		}},
	}
}

func writeRawIndex(t *testing.T, root string, idx Index) {
	t.Helper()

	path := ArtifactPath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	defer file.Close()
	if err := gob.NewEncoder(file).Encode(idx); err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
}

func writeStorageFile(t *testing.T, root string, name string, contents string) {
	t.Helper()

	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

func assertFileContents(t *testing.T, root string, name string, want string) {
	t.Helper()

	path := filepath.Join(root, filepath.FromSlash(name))
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", name, err)
	}
	if string(got) != want {
		t.Fatalf("%s = %q, want %q", name, string(got), want)
	}
}

package search

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/procommerz/speecdex-search/internal/storage"
)

func TestSemanticSearchRanksByDescendingSimilarityAndDefaultLimit(t *testing.T) {
	t.Parallel()

	idx := testIndex(24)
	embedder := &fakeEmbedder{vectors: [][]float64{{1, 0}}}

	got, err := Run(context.Background(), idx, Options{Query: "root"}, embedder)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(got) != DefaultLimit {
		t.Fatalf("results length = %d, want default limit %d", len(got), DefaultLimit)
	}
	if got[0].ChunkID != "chunk-00" {
		t.Fatalf("first chunk = %q, want chunk-00", got[0].ChunkID)
	}
	if got[0].Score < got[1].Score {
		t.Fatalf("results are not descending: %#v then %#v", got[0], got[1])
	}
	if !reflect.DeepEqual(got[0].MatchSources, []string{"semantic"}) {
		t.Fatalf("MatchSources = %#v, want semantic", got[0].MatchSources)
	}
	if embedder.calls != 1 {
		t.Fatalf("embedder calls = %d, want 1", embedder.calls)
	}
}

func TestLiteralOnlySearchUsesORTermsWithoutEmbedding(t *testing.T) {
	t.Parallel()

	idx := testIndex(3)
	idx.Chunks[0].Text = "extends BusinessObject"
	idx.Chunks[1].Text = "implements BusinessObject"
	idx.Chunks[2].Text = "unrelated"
	embedder := &fakeEmbedder{vectors: [][]float64{{1, 0}}}

	got, err := Run(context.Background(), idx, Options{Text: []string{"extends BusinessObject", "implements BusinessObject"}}, embedder)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("results length = %d, want 2: %#v", len(got), got)
	}
	if embedder.calls != 0 {
		t.Fatalf("embedder calls = %d, want 0", embedder.calls)
	}
	if got[0].Score != 1 || !reflect.DeepEqual(got[0].MatchSources, []string{"text"}) {
		t.Fatalf("first result = %#v, want literal-only score/source", got[0])
	}
	if !reflect.DeepEqual(got[0].MatchedText, []string{"extends BusinessObject"}) {
		t.Fatalf("MatchedText = %#v, want first literal", got[0].MatchedText)
	}
}

func TestCombinedSearchDeduplicatesAndPreservesSources(t *testing.T) {
	t.Parallel()

	idx := testIndex(3)
	idx.Chunks[0].Text = "semantic and extends BusinessObject"
	idx.Chunks[1].Text = "semantic only"
	idx.Chunks[2].Text = "text only implements BusinessObject"

	got, err := Run(context.Background(), idx, Options{
		Query: "root",
		Text:  []string{"extends BusinessObject", "implements BusinessObject"},
		Limit: 3,
	}, &fakeEmbedder{vectors: [][]float64{{1, 0}}})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	byID := map[string]Result{}
	for _, result := range got {
		byID[result.ChunkID] = result
	}
	if len(byID) != len(got) {
		t.Fatalf("results contain duplicate chunk IDs: %#v", got)
	}
	if !reflect.DeepEqual(byID["chunk-00"].MatchSources, []string{"semantic", "text"}) {
		t.Fatalf("chunk-00 sources = %#v, want semantic+text", byID["chunk-00"].MatchSources)
	}
	if !reflect.DeepEqual(byID["chunk-00"].MatchedText, []string{"extends BusinessObject"}) {
		t.Fatalf("chunk-00 matched text = %#v", byID["chunk-00"].MatchedText)
	}
	if !reflect.DeepEqual(byID["chunk-02"].MatchSources, []string{"text"}) {
		t.Fatalf("chunk-02 sources = %#v, want text", byID["chunk-02"].MatchSources)
	}
}

func TestSearchTieBreaksByPathLineAndChunkID(t *testing.T) {
	t.Parallel()

	idx := storage.Index{
		Header: storage.Header{EmbeddingDimensions: 2},
		Chunks: []storage.Chunk{
			{ID: "b", SourcePath: "b.md", StartLine: 1, EndLine: 1, Text: "match", Vector: []float64{1, 0}},
			{ID: "a", SourcePath: "a.md", StartLine: 2, EndLine: 2, Text: "match", Vector: []float64{1, 0}},
			{ID: "c", SourcePath: "a.md", StartLine: 1, EndLine: 1, Text: "match", Vector: []float64{1, 0}},
		},
	}

	got, err := Run(context.Background(), idx, Options{Query: "root", Limit: 3}, &fakeEmbedder{vectors: [][]float64{{1, 0}}})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	gotIDs := []string{got[0].ChunkID, got[1].ChunkID, got[2].ChunkID}
	wantIDs := []string{"c", "a", "b"}
	if !reflect.DeepEqual(gotIDs, wantIDs) {
		t.Fatalf("result IDs = %#v, want %#v", gotIDs, wantIDs)
	}
}

func TestWriteYAMLEmitsFencedResultsAndEmptyResults(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	results := []Result{{
		File:         "docs/example.md",
		StartLine:    10,
		EndLine:      24,
		Score:        0.8123,
		MatchSources: []string{"semantic", "text"},
		MatchedText:  []string{"extends BusinessObject"},
		Text:         "Chunk text appears here.",
	}}

	if err := WriteYAML(&out, results); err != nil {
		t.Fatalf("WriteYAML() error = %v", err)
	}
	got := out.String()
	for _, want := range []string{
		"```yaml\n",
		"results:",
		"file: docs/example.md",
		"start_line: 10",
		"match_sources:",
		"- semantic",
		"matched_text:",
		"text: Chunk text appears here.",
		"```\n",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("YAML output = %q, want to contain %q", got, want)
		}
	}

	out.Reset()
	if err := WriteYAML(&out, nil); err != nil {
		t.Fatalf("WriteYAML(empty) error = %v", err)
	}
	if out.String() != "```yaml\nresults: []\n```\n" {
		t.Fatalf("empty YAML output = %q, want fenced empty results", out.String())
	}
}

func testIndex(chunks int) storage.Index {
	idx := storage.Index{
		Header: storage.Header{
			FormatName:             storage.FormatName,
			FormatVersion:          storage.FormatVersion,
			CreatedAt:              time.Date(2026, 5, 20, 8, 0, 0, 0, time.UTC),
			ProjectRootIdentity:    "/tmp/project",
			EmbeddingProviderStyle: "openai-compatible",
			EmbeddingModelIdentity: "test-model",
			EmbeddingDimensions:    2,
			DistanceMetric:         storage.DistanceMetricCosine,
		},
	}
	for i := 0; i < chunks; i++ {
		angle := float64(i) * math.Pi / float64(chunks)
		idx.Chunks = append(idx.Chunks, storage.Chunk{
			ID:         "chunk-" + twoDigit(i),
			SourcePath: "docs/" + twoDigit(i) + ".md",
			StartLine:  i + 1,
			EndLine:    i + 1,
			Text:       "chunk text " + twoDigit(i),
			Vector:     []float64{math.Cos(angle), math.Sin(angle)},
		})
	}
	return idx
}

func twoDigit(value int) string {
	return fmt.Sprintf("%02d", value)
}

type fakeEmbedder struct {
	vectors [][]float64
	calls   int
}

func (f *fakeEmbedder) Embed(context.Context, []string) ([][]float64, error) {
	f.calls++
	return f.vectors, nil
}

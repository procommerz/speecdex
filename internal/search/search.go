package search

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/procommerz/speecdex-search/internal/storage"
)

const (
	DefaultLimit = 10

	matchSourceSemantic = "semantic"
	matchSourceText     = "text"
)

type Options struct {
	Query string
	Text  []string
	Limit int
}

type Embedder interface {
	Embed(ctx context.Context, inputs []string) ([][]float64, error)
}

type Result struct {
	ChunkID      string
	File         string
	StartLine    int
	EndLine      int
	Score        float64
	MatchSources []string
	MatchedText  []string
	Text         string
}

func Run(ctx context.Context, idx storage.Index, opts Options, embedder Embedder) ([]Result, error) {
	query := strings.TrimSpace(opts.Query)
	textTerms := nonEmptyTerms(opts.Text)
	if query == "" && len(textTerms) == 0 {
		return nil, errors.New("search requires --query or --text")
	}

	limit := opts.Limit
	if limit <= 0 {
		limit = DefaultLimit
	}

	results := make(map[string]*Result)
	if query != "" {
		if embedder == nil {
			return nil, errors.New("semantic search requires an embedding provider")
		}
		vectors, err := embedder.Embed(ctx, []string{query})
		if err != nil {
			return nil, fmt.Errorf("embed search query: %w", err)
		}
		if len(vectors) != 1 {
			return nil, fmt.Errorf("embed search query: got %d vectors, want 1", len(vectors))
		}
		if len(vectors[0]) != idx.Header.EmbeddingDimensions {
			return nil, fmt.Errorf("query embedding has %d dimensions, want %d", len(vectors[0]), idx.Header.EmbeddingDimensions)
		}

		for _, scored := range semanticCandidates(idx.Chunks, vectors[0], limit) {
			if len(textTerms) > 0 && scored.score <= 0 {
				continue
			}
			result := resultForChunk(scored.chunk)
			result.Score = scored.score
			addSource(result, matchSourceSemantic)
			results[scored.chunk.ID] = result
		}
	}

	for _, chunk := range idx.Chunks {
		matched := matchedTerms(chunk.Text, textTerms)
		if len(matched) == 0 {
			continue
		}

		result := results[chunk.ID]
		if result == nil {
			result = resultForChunk(chunk)
			if query == "" {
				result.Score = 1
			}
			results[chunk.ID] = result
		}
		addSource(result, matchSourceText)
		result.MatchedText = appendUnique(result.MatchedText, matched)
	}

	ranked := flattenResults(results)
	if len(ranked) > limit {
		ranked = ranked[:limit]
	}
	return ranked, nil
}

func semanticCandidates(chunks []storage.Chunk, queryVector []float64, limit int) []scoredChunk {
	scored := make([]scoredChunk, 0, len(chunks))
	for _, chunk := range chunks {
		scored = append(scored, scoredChunk{
			chunk: chunk,
			score: cosineSimilarity(queryVector, chunk.Vector),
		})
	}
	sort.SliceStable(scored, func(i, j int) bool {
		return compareScored(scored[i], scored[j])
	})
	if limit > 0 && len(scored) > limit {
		return scored[:limit]
	}
	return scored
}

func cosineSimilarity(a []float64, b []float64) float64 {
	var dot, normA, normB float64
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

func resultForChunk(chunk storage.Chunk) *Result {
	return &Result{
		ChunkID:   chunk.ID,
		File:      chunk.SourcePath,
		StartLine: chunk.StartLine,
		EndLine:   chunk.EndLine,
		Text:      chunk.Text,
	}
}

func flattenResults(results map[string]*Result) []Result {
	ranked := make([]Result, 0, len(results))
	for _, result := range results {
		ranked = append(ranked, *result)
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		return compareResults(ranked[i], ranked[j])
	})
	return ranked
}

func compareScored(a scoredChunk, b scoredChunk) bool {
	if a.score != b.score {
		return a.score > b.score
	}
	return compareChunk(a.chunk, b.chunk)
}

func compareResults(a Result, b Result) bool {
	if a.Score != b.Score {
		return a.Score > b.Score
	}
	if a.File != b.File {
		return a.File < b.File
	}
	if a.StartLine != b.StartLine {
		return a.StartLine < b.StartLine
	}
	if a.EndLine != b.EndLine {
		return a.EndLine < b.EndLine
	}
	return a.ChunkID < b.ChunkID
}

func compareChunk(a storage.Chunk, b storage.Chunk) bool {
	if a.SourcePath != b.SourcePath {
		return a.SourcePath < b.SourcePath
	}
	if a.StartLine != b.StartLine {
		return a.StartLine < b.StartLine
	}
	if a.EndLine != b.EndLine {
		return a.EndLine < b.EndLine
	}
	return a.ID < b.ID
}

func addSource(result *Result, source string) {
	for _, existing := range result.MatchSources {
		if existing == source {
			return
		}
	}
	result.MatchSources = append(result.MatchSources, source)
	sort.SliceStable(result.MatchSources, func(i, j int) bool {
		return sourceOrder(result.MatchSources[i]) < sourceOrder(result.MatchSources[j])
	})
}

func sourceOrder(source string) int {
	switch source {
	case matchSourceSemantic:
		return 0
	case matchSourceText:
		return 1
	default:
		return 2
	}
}

func nonEmptyTerms(terms []string) []string {
	out := make([]string, 0, len(terms))
	for _, term := range terms {
		if strings.TrimSpace(term) != "" {
			out = append(out, term)
		}
	}
	return out
}

func matchedTerms(text string, terms []string) []string {
	matches := make([]string, 0, len(terms))
	for _, term := range terms {
		if strings.Contains(text, term) {
			matches = append(matches, term)
		}
	}
	return matches
}

func appendUnique(existing []string, values []string) []string {
	for _, value := range values {
		seen := false
		for _, current := range existing {
			if current == value {
				seen = true
				break
			}
		}
		if !seen {
			existing = append(existing, value)
		}
	}
	return existing
}

type scoredChunk struct {
	chunk storage.Chunk
	score float64
}

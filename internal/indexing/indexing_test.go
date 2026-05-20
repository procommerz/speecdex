package indexing

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDiscoverMarkdownFindsMarkdownFilesInStableOrder(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile(t, root, "b/second.MARKDOWN", "# second\r\nbody\r\n")
	writeFile(t, root, "a/first.md", "# first\nbody\n")
	writeFile(t, root, "notes.txt", "# not markdown\n")

	got, err := DiscoverMarkdown(Options{ProjectRoot: root})
	if err != nil {
		t.Fatalf("DiscoverMarkdown() error = %v", err)
	}

	paths := filePaths(got.Files)
	want := []string{"a/first.md", "b/second.MARKDOWN"}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("paths = %#v, want %#v", paths, want)
	}
	if got.Files[1].Text != "# second\nbody\n" {
		t.Fatalf("normalized text = %q, want CRLF normalized", got.Files[1].Text)
	}
	wantHash := sha256.Sum256([]byte("# second\nbody\n"))
	if got.Files[1].ContentHash != hex.EncodeToString(wantHash[:]) {
		t.Fatalf("ContentHash = %q, want hash of normalized text", got.Files[1].ContentHash)
	}
	if got.Files[1].Size == 0 {
		t.Fatal("Size = 0, want file size metadata")
	}
	if got.Files[1].ModifiedAt.IsZero() {
		t.Fatal("ModifiedAt is zero, want file modified timestamp")
	}
}

func TestDiscoverMarkdownAppliesIgnoreRules(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile(t, root, ".git/ignored.md", "git\n")
	writeFile(t, root, ".speecdex/indexed-never.md", "artifact\n")
	writeFile(t, root, "ignored.md", "file name\n")
	writeFile(t, root, "docs/private/hidden.md", "relative directory\n")
	writeFile(t, root, "docs/plans/hidden.md", "glob\n")
	writeFile(t, root, "src/ignored.md", "exact file name below root\n")
	writeFile(t, root, "docs/keep.md", "keep\n")

	got, err := DiscoverMarkdown(Options{
		ProjectRoot: root,
		IgnoredEntries: []string{
			".git",
			"ignored.md",
			"docs/private",
			"*/plans/*.md",
		},
	})
	if err != nil {
		t.Fatalf("DiscoverMarkdown() error = %v", err)
	}

	paths := filePaths(got.Files)
	want := []string{"docs/keep.md"}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("paths = %#v, want %#v", paths, want)
	}
	if got.IgnoredEntriesCount != 4 {
		t.Fatalf("IgnoredEntriesCount = %d, want 4", got.IgnoredEntriesCount)
	}
}

func TestDiscoverMarkdownAppliesOnlyEntriesBeforeIgnoreRules(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile(t, root, ".speecdex/included-never.md", "artifact\n")
	writeFile(t, root, "docs/keep.md", "keep\n")
	writeFile(t, root, "docs/private/secret.md", "secret\n")
	writeFile(t, root, "src/keep.md", "outside include\n")
	writeFile(t, root, "release.md", "basename include\n")
	writeFile(t, root, "notes.txt", "not markdown\n")

	got, err := DiscoverMarkdown(Options{
		ProjectRoot: root,
		OnlyEntries: []string{
			"docs",
			"release.md",
			".speecdex",
		},
		IgnoredEntries: []string{
			"docs/private",
		},
	})
	if err != nil {
		t.Fatalf("DiscoverMarkdown() error = %v", err)
	}

	paths := filePaths(got.Files)
	want := []string{"docs/keep.md", "release.md"}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("paths = %#v, want %#v", paths, want)
	}
	if got.IgnoredEntriesCount != 1 {
		t.Fatalf("IgnoredEntriesCount = %d, want 1", got.IgnoredEntriesCount)
	}
}

func TestDiscoverMarkdownRejectsInvalidUTF8(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "bad.md")
	if err := os.WriteFile(path, []byte{0xff, 0xfe, '\n'}, 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := DiscoverMarkdown(Options{ProjectRoot: root})
	if err == nil {
		t.Fatal("DiscoverMarkdown() error = nil, want invalid UTF-8 error")
	}
	if !strings.Contains(err.Error(), "bad.md") || !strings.Contains(err.Error(), "UTF-8") {
		t.Fatalf("error = %q, want path and UTF-8 message", err.Error())
	}
}

func TestDiscoverMarkdownRejectsMalformedOnlyEntriesGlob(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile(t, root, "docs/keep.md", "keep\n")

	_, err := DiscoverMarkdown(Options{
		ProjectRoot: root,
		OnlyEntries: []string{"docs/[unclosed"},
	})
	if err == nil {
		t.Fatal("DiscoverMarkdown() error = nil, want malformed include pattern error")
	}
	if !strings.Contains(err.Error(), "config.only_entries") || !strings.Contains(err.Error(), "docs/[unclosed") {
		t.Fatalf("error = %q, want config field and bad pattern", err.Error())
	}
}

func TestDiscoverMarkdownRejectsMalformedIgnoreGlob(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile(t, root, "docs/keep.md", "keep\n")

	_, err := DiscoverMarkdown(Options{
		ProjectRoot:    root,
		IgnoredEntries: []string{"docs/[unclosed"},
	})
	if err == nil {
		t.Fatal("DiscoverMarkdown() error = nil, want malformed ignore pattern error")
	}
	if !strings.Contains(err.Error(), "config.ignored_entries") || !strings.Contains(err.Error(), "docs/[unclosed") {
		t.Fatalf("error = %q, want config field and bad pattern", err.Error())
	}
}

func TestChunkMarkdownProducesOverlappingNonEmptyChunksAndLineRanges(t *testing.T) {
	t.Parallel()

	file := MarkdownFile{
		RelativePath: "docs/example.md",
		Text: strings.Join([]string{
			"# Title",
			"",
			"alpha paragraph begins here",
			"alpha paragraph continues here",
			"",
			"## Next",
			"beta paragraph begins here",
			"beta paragraph continues here",
		}, "\n"),
	}

	chunks := ChunkMarkdown(file, Options{
		ChunkSize:              70,
		ChunkOverlap:           30,
		EmbeddingModelIdentity: "embedder-a",
	})
	if len(chunks) < 2 {
		t.Fatalf("len(chunks) = %d, want overlapping chunks", len(chunks))
	}
	for _, chunk := range chunks {
		if strings.TrimSpace(chunk.Text) == "" {
			t.Fatalf("empty chunk: %#v", chunk)
		}
		if chunk.StartLine < 1 || chunk.EndLine < chunk.StartLine {
			t.Fatalf("invalid line range: %#v", chunk)
		}
		if chunk.SourcePath != file.RelativePath {
			t.Fatalf("SourcePath = %q, want %q", chunk.SourcePath, file.RelativePath)
		}
	}
	if chunks[1].StartLine > chunks[0].EndLine {
		t.Fatalf("chunks do not overlap: first=%#v second=%#v", chunks[0], chunks[1])
	}
}

func TestChunkMarkdownHandlesManyShortHeadingSections(t *testing.T) {
	t.Parallel()

	file := MarkdownFile{
		RelativePath: "docs/sections.md",
		Text: strings.Join([]string{
			"# Root",
			"intro",
			"## One",
			"alpha",
			"### Two",
			"bravo",
			"## Three",
			"charlie",
			"### Four",
			"delta",
			"## Five",
			"echo",
		}, "\n"),
	}

	first := ChunkMarkdown(file, Options{ChunkSize: 24, ChunkOverlap: 6})
	second := ChunkMarkdown(file, Options{ChunkSize: 24, ChunkOverlap: 6})
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("chunks differ across identical runs:\nfirst=%#v\nsecond=%#v", first, second)
	}
	if len(first) < 3 {
		t.Fatalf("len(chunks) = %d, want several chunks for short heading sections", len(first))
	}
	for i, chunk := range first {
		if strings.TrimSpace(chunk.Text) == "" {
			t.Fatalf("chunk %d is empty: %#v", i, chunk)
		}
		if chunk.StartLine < 1 || chunk.EndLine < chunk.StartLine {
			t.Fatalf("chunk %d has invalid range: %#v", i, chunk)
		}
		if i > 0 && chunk.StartLine <= first[i-1].StartLine {
			t.Fatalf("chunk %d did not advance: prev=%#v current=%#v", i, first[i-1], chunk)
		}
	}
}

func TestChooseChunkEndIgnoresHeadingsInsideFencedCode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		lines     []string
		chunkSize int
		want      int
	}{
		{
			name: "backtick fence",
			lines: []string{
				"intro paragraph",
				"```",
				"# not a heading",
				"code body with enough text",
			},
			chunkSize: 42,
			want:      3,
		},
		{
			name: "tilde fence",
			lines: []string{
				"intro paragraph",
				"~~~",
				"# not a heading",
				"code body with enough text",
			},
			chunkSize: 38,
			want:      3,
		},
		{
			name: "fence with info string",
			lines: []string{
				"intro paragraph",
				"```bash",
				"# shell comment",
				"echo enough text here",
			},
			chunkSize: 40,
			want:      3,
		},
		{
			name: "one-space indented fence",
			lines: []string{
				"intro paragraph",
				" ```yaml",
				"# yaml comment",
				"key: value with text",
			},
			chunkSize: 42,
			want:      3,
		},
		{
			name: "two-space indented fence",
			lines: []string{
				"intro paragraph",
				"  ~~~yaml",
				"# yaml comment",
				"key: value with text",
			},
			chunkSize: 42,
			want:      3,
		},
		{
			name: "three-space indented fence",
			lines: []string{
				"intro paragraph",
				"   ```yaml",
				"# yaml comment",
				"key: value with text",
			},
			chunkSize: 43,
			want:      3,
		},
		{
			name: "unterminated fence",
			lines: []string{
				"intro paragraph",
				"```",
				"# still code",
				"code body",
				"## also code",
				"trailing text that overflows the chunk",
			},
			chunkSize: 65,
			want:      5,
		},
		{
			name: "heading after closed fence",
			lines: []string{
				"intro paragraph",
				"```",
				"# not a heading",
				"```",
				"## Real Heading",
				"real heading body long enough to overflow",
			},
			chunkSize: 48,
			want:      4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := chooseChunkEnd(tt.lines, 0, tt.chunkSize); got != tt.want {
				t.Fatalf("chooseChunkEnd() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestChunkMarkdownNoFenceFixtureIDsRemainStable(t *testing.T) {
	t.Parallel()

	file := MarkdownFile{
		RelativePath: "docs/no-fences.md",
		Text: strings.Join([]string{
			"# Root",
			"intro",
			"## One",
			"alpha",
			"### Two",
			"bravo",
			"## Three",
			"charlie",
		}, "\n"),
	}

	chunks := ChunkMarkdown(file, Options{ChunkSize: 24, ChunkOverlap: 6})
	got := make([]string, len(chunks))
	for i, chunk := range chunks {
		got[i] = chunk.ID
	}
	want := []string{
		"f9319e91b99b73582e305e8094c73d5615f6543b80b5f424e5bf052fcbb3aca2",
		"061eb31db56600c28489d48485b51caa4256f9c6d477262262b28b19084ad95c",
		"4aea1fa415ff9e1ea931b7d23c228c7426ed41d434f91e6e0058fed830dc5e1d",
		"16e1b9e8da0047dc81bd3c60782fe18c6dc0a7aff5ad30ccb69c1e194e004e6b",
		"6c204f1ca00b99ef245938c22d3c16ca2f6a8e5b70bf57d4840a11aedda10608",
		"d4c3b3980fde9d03dfd97276771e55af6fbdd71670b2e49421a702add76de1e2",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("chunk IDs = %#v, want %#v", got, want)
	}
}

func TestChunkMarkdownDiscardsStandaloneTableSeparatorBeforeLongRow(t *testing.T) {
	t.Parallel()

	file := MarkdownFile{
		RelativePath: "docs/changelog.md",
		Text: strings.Join([]string{
			"---",
			"",
			"## Changelog",
			"",
			"| Date | Change |",
			"|---|---|",
			"| 2026-05-11 | " + strings.Repeat("Spec sweep pass changed behavior. ", 8) + "|",
		}, "\n"),
	}

	chunks := ChunkMarkdown(file, Options{ChunkSize: 24, ChunkOverlap: 8})
	if len(chunks) == 0 {
		t.Fatal("len(chunks) = 0, want chunks with meaningful changelog content")
	}
	for _, chunk := range chunks {
		if strings.TrimSpace(chunk.Text) == "|---|---|" {
			t.Fatalf("got standalone table separator chunk: %#v", chunk)
		}
		if !isMeaningfulChunkText(chunk.Text) {
			t.Fatalf("got meaningless chunk: %#v", chunk)
		}
	}
}

func TestChunkMarkdownDiscardsPunctuationOnlyChunks(t *testing.T) {
	t.Parallel()

	file := MarkdownFile{
		RelativePath: "docs/punctuation.md",
		Text: strings.Join([]string{
			"---",
			"|---|---|",
			"{}",
			"| 2026-05-11 | Change |",
		}, "\n"),
	}

	chunks := ChunkMarkdown(file, Options{ChunkSize: 20, ChunkOverlap: 0})
	if len(chunks) == 0 {
		t.Fatal("len(chunks) = 0, want alphanumeric table row chunk")
	}
	for _, chunk := range chunks {
		if !isMeaningfulChunkText(chunk.Text) {
			t.Fatalf("got punctuation-only chunk: %#v", chunk)
		}
	}
	if !strings.Contains(chunkTextFromChunks(chunks), "2026") && !strings.Contains(chunkTextFromChunks(chunks), "Change") {
		t.Fatalf("chunks = %#v, want preserved alphanumeric table content", chunks)
	}
}

func TestChunkMarkdownIDsAreDeterministicAndModelSpecific(t *testing.T) {
	t.Parallel()

	file := MarkdownFile{
		RelativePath: "docs/example.md",
		Text:         "line one\nline two\nline three",
	}
	opts := Options{ChunkSize: 12, ChunkOverlap: 4, EmbeddingModelIdentity: "model-a"}

	first := ChunkMarkdown(file, opts)
	second := ChunkMarkdown(file, opts)
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("chunks differ across identical runs:\nfirst=%#v\nsecond=%#v", first, second)
	}

	third := ChunkMarkdown(file, Options{ChunkSize: 12, ChunkOverlap: 4, EmbeddingModelIdentity: "model-b"})
	if len(first) == 0 || len(third) == 0 {
		t.Fatalf("got no chunks: first=%d third=%d", len(first), len(third))
	}
	if first[0].ID == third[0].ID {
		t.Fatalf("chunk ID = %q for two model identities, want different IDs", first[0].ID)
	}
}

func TestChunkIDLengthPrefixesHashFields(t *testing.T) {
	t.Parallel()

	left := newChunk("docs/example.md", 1, 2, "a\x00b", "c")
	right := newChunk("docs/example.md", 1, 2, "a", "b\x00c")

	if left.ID == right.ID {
		t.Fatalf("chunk IDs are equal for ambiguous NUL-containing fields: %q", left.ID)
	}
}

func TestChunkMarkdownSplitsOnlyLongLines(t *testing.T) {
	t.Parallel()

	file := MarkdownFile{
		RelativePath: "docs/long-line.md",
		Text:         "abcdefghijklmnopqrstuvwxy",
	}
	chunks := ChunkMarkdown(file, Options{ChunkSize: 10, ChunkOverlap: 3})
	if len(chunks) != 4 {
		t.Fatalf("len(chunks) = %d, want 4", len(chunks))
	}
	for _, chunk := range chunks {
		if chunk.StartLine != 1 || chunk.EndLine != 1 {
			t.Fatalf("line range = %d-%d, want 1-1", chunk.StartLine, chunk.EndLine)
		}
		if len([]rune(chunk.Text)) > 10 {
			t.Fatalf("chunk text length = %d, want <= 10", len([]rune(chunk.Text)))
		}
	}
	if chunks[0].Text[7:] != chunks[1].Text[:3] {
		t.Fatalf("adjacent chunks = %q and %q, want 3-character overlap", chunks[0].Text, chunks[1].Text)
	}
}

func filePaths(files []MarkdownFile) []string {
	paths := make([]string, len(files))
	for i, file := range files {
		paths[i] = file.RelativePath
	}
	return paths
}

func chunkTextFromChunks(chunks []Chunk) string {
	texts := make([]string, len(chunks))
	for i, chunk := range chunks {
		texts[i] = chunk.Text
	}
	return strings.Join(texts, "\n")
}

func writeFile(t *testing.T, root string, name string, contents string) {
	t.Helper()

	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

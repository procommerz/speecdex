package indexing

import (
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

package indexing

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/procommerz/speecdex-search/internal/config"
)

type Options struct {
	ProjectRoot            string
	IgnoredEntries         []string
	ChunkSize              int
	ChunkOverlap           int
	EmbeddingModelIdentity string
}

type DiscoveryResult struct {
	Files               []MarkdownFile
	IgnoredEntriesCount int
}

type MarkdownFile struct {
	RelativePath string
	AbsolutePath string
	Text         string
	ContentHash  string
	Size         int64
	ModifiedAt   time.Time
}

type Chunk struct {
	ID            string
	SourcePath    string
	StartLine     int
	EndLine       int
	Text          string
	ModelIdentity string
}

type Error struct {
	Path    string
	Message string
	Err     error
}

func (e *Error) Error() string {
	if e.Path == "" {
		return e.Message
	}
	if e.Err == nil {
		return fmt.Sprintf("%s: %s", e.Path, e.Message)
	}
	return fmt.Sprintf("%s: %s: %v", e.Path, e.Message, e.Err)
}

func (e *Error) Unwrap() error {
	return e.Err
}

func DiscoverMarkdown(opts Options) (DiscoveryResult, error) {
	root := opts.ProjectRoot
	if root == "" {
		wd, err := os.Getwd()
		if err != nil {
			return DiscoveryResult{}, &Error{Message: "determine project root", Err: err}
		}
		root = wd
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return DiscoveryResult{}, &Error{Path: root, Message: "resolve project root", Err: err}
	}

	matcher, err := newIgnoreMatcher(opts.IgnoredEntries)
	if err != nil {
		return DiscoveryResult{}, err
	}
	result := DiscoveryResult{IgnoredEntriesCount: len(opts.IgnoredEntries)}

	err = filepath.WalkDir(absRoot, func(currentPath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return &Error{Path: currentPath, Message: "walk project tree", Err: walkErr}
		}

		if currentPath == absRoot {
			return nil
		}

		rel, err := filepath.Rel(absRoot, currentPath)
		if err != nil {
			return &Error{Path: currentPath, Message: "determine relative path", Err: err}
		}
		rel = filepath.ToSlash(rel)

		if matcher.Match(rel, entry.IsDir()) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if entry.IsDir() || !isMarkdownPath(rel) {
			return nil
		}

		info, err := entry.Info()
		if err != nil {
			return &Error{Path: rel, Message: "read file metadata", Err: err}
		}

		data, err := os.ReadFile(currentPath)
		if err != nil {
			return &Error{Path: rel, Message: "read Markdown file", Err: err}
		}
		if !utf8.Valid(data) {
			return &Error{Path: rel, Message: "read Markdown file as UTF-8"}
		}

		text := normalizeLineEndings(string(data))
		result.Files = append(result.Files, MarkdownFile{
			RelativePath: rel,
			AbsolutePath: currentPath,
			Text:         text,
			ContentHash:  contentHash(text),
			Size:         info.Size(),
			ModifiedAt:   info.ModTime(),
		})
		return nil
	})
	if err != nil {
		return DiscoveryResult{}, err
	}

	sort.Slice(result.Files, func(i, j int) bool {
		return result.Files[i].RelativePath < result.Files[j].RelativePath
	})
	return result, nil
}

func ChunkMarkdown(file MarkdownFile, opts Options) []Chunk {
	chunkSize := opts.ChunkSize
	if chunkSize <= 0 {
		chunkSize = config.DefaultChunkSize
	}
	chunkOverlap := opts.ChunkOverlap
	if chunkOverlap < 0 {
		chunkOverlap = 0
	}
	if chunkOverlap >= chunkSize {
		chunkOverlap = chunkSize - 1
	}

	lines := splitLogicalLines(file.Text)
	var chunks []Chunk
	for i := 0; i < len(lines); {
		lineText := lines[i]
		if lineText == "" {
			i++
			continue
		}

		if runeLen(lineText) > chunkSize {
			chunks = append(chunks, splitLongLine(file.RelativePath, i+1, lineText, chunkSize, chunkOverlap, opts.EmbeddingModelIdentity)...)
			i++
			continue
		}

		end := chooseChunkEnd(lines, i, chunkSize)
		text := chunkText(lines[i:end])
		if strings.TrimSpace(text) != "" {
			chunks = append(chunks, newChunk(file.RelativePath, i+1, end, text, opts.EmbeddingModelIdentity))
		}

		if end >= len(lines) {
			break
		}
		next := overlapStart(lines, i, end, chunkOverlap)
		if next <= i {
			next = end
		}
		i = next
	}

	return chunks
}

func ChunkFiles(files []MarkdownFile, opts Options) []Chunk {
	var chunks []Chunk
	for _, file := range files {
		chunks = append(chunks, ChunkMarkdown(file, opts)...)
	}
	return chunks
}

func isMarkdownPath(rel string) bool {
	switch strings.ToLower(filepath.Ext(rel)) {
	case ".md", ".markdown":
		return true
	default:
		return false
	}
}

func normalizeLineEndings(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	return strings.ReplaceAll(text, "\r", "\n")
}

func contentHash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

func splitLogicalLines(text string) []string {
	if text == "" {
		return nil
	}
	lines := strings.Split(text, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func chooseChunkEnd(lines []string, start int, chunkSize int) int {
	size := 0
	lastPreferredEnd := 0
	for i := start; i < len(lines); i++ {
		lineSize := runeLen(lines[i])
		if i > start {
			lineSize++
		}
		if size > 0 && size+lineSize > chunkSize {
			if lastPreferredEnd > start && chunkTextLen(lines[start:lastPreferredEnd]) >= chunkSize/2 {
				return lastPreferredEnd
			}
			return i
		}

		size += lineSize
		if strings.TrimSpace(lines[i]) == "" {
			lastPreferredEnd = i + 1
		}
		// TODO: Track fenced code blocks before treating Markdown headings in code
		// fences differently from document headings.
		if i+1 < len(lines) && isHeading(lines[i+1]) && i+1 > start {
			lastPreferredEnd = i + 1
		}
	}
	return len(lines)
}

func overlapStart(lines []string, start int, end int, chunkOverlap int) int {
	if chunkOverlap <= 0 || end-start <= 1 {
		return end
	}

	size := 0
	for i := end - 1; i > start; i-- {
		if strings.TrimSpace(lines[i]) == "" && size > 0 {
			return i + 1
		}
		if size > 0 {
			size++
		}
		size += runeLen(lines[i])
		if size >= chunkOverlap {
			return i
		}
	}
	return end - 1
}

func splitLongLine(sourcePath string, lineNumber int, text string, chunkSize int, chunkOverlap int, modelIdentity string) []Chunk {
	runes := []rune(text)
	if chunkSize <= 0 {
		chunkSize = len(runes)
	}
	step := chunkSize - chunkOverlap
	if step <= 0 {
		step = chunkSize
	}

	var chunks []Chunk
	for start := 0; start < len(runes); {
		end := start + chunkSize
		if end > len(runes) {
			end = len(runes)
		}
		part := string(runes[start:end])
		if strings.TrimSpace(part) != "" {
			chunks = append(chunks, newChunk(sourcePath, lineNumber, lineNumber, part, modelIdentity))
		}
		if end == len(runes) {
			break
		}
		start += step
	}
	return chunks
}

func chunkText(lines []string) string {
	return strings.Join(lines, "\n")
}

func chunkTextLen(lines []string) int {
	return runeLen(chunkText(lines))
}

func runeLen(text string) int {
	return len([]rune(text))
}

func isHeading(line string) bool {
	trimmed := strings.TrimLeft(line, " \t")
	if trimmed == "" || trimmed[0] != '#' {
		return false
	}
	hashes := 0
	for hashes < len(trimmed) && trimmed[hashes] == '#' {
		hashes++
	}
	return hashes <= 6 && hashes < len(trimmed) && trimmed[hashes] == ' '
}

func newChunk(sourcePath string, startLine int, endLine int, text string, modelIdentity string) Chunk {
	chunk := Chunk{
		SourcePath:    sourcePath,
		StartLine:     startLine,
		EndLine:       endLine,
		Text:          text,
		ModelIdentity: modelIdentity,
	}
	chunk.ID = chunkID(chunk)
	return chunk
}

func chunkID(chunk Chunk) string {
	hash := sha256.New()
	writeHashPart(hash, chunk.SourcePath)
	writeHashPart(hash, strconv.Itoa(chunk.StartLine))
	writeHashPart(hash, strconv.Itoa(chunk.EndLine))
	writeHashPart(hash, chunk.Text)
	writeHashPart(hash, chunk.ModelIdentity)
	return hex.EncodeToString(hash.Sum(nil))
}

func writeHashPart(hash io.Writer, value string) {
	_, _ = io.WriteString(hash, strconv.Itoa(len(value)))
	_, _ = io.WriteString(hash, ":")
	_, _ = io.WriteString(hash, value)
}

type ignoreMatcher struct {
	patterns []ignorePattern
}

type ignorePattern struct {
	raw     string
	hasGlob bool
	hasPath bool
}

func newIgnoreMatcher(entries []string) (ignoreMatcher, error) {
	patterns := []ignorePattern{
		{raw: ".speecdex", hasPath: false},
	}
	for _, entry := range entries {
		normalized := normalizeIgnoreEntry(entry)
		if normalized == "" || normalized == "." {
			continue
		}
		if err := validateIgnorePattern(normalized); err != nil {
			return ignoreMatcher{}, &Error{
				Message: fmt.Sprintf("invalid config.ignored_entries pattern %q", normalized),
				Err:     err,
			}
		}
		patterns = append(patterns, ignorePattern{
			raw:     normalized,
			hasGlob: strings.Contains(normalized, "*"),
			hasPath: strings.Contains(normalized, "/"),
		})
	}
	return ignoreMatcher{patterns: patterns}, nil
}

func (m ignoreMatcher) Match(rel string, isDir bool) bool {
	rel = path.Clean(filepath.ToSlash(rel))
	for _, pattern := range m.patterns {
		if pattern.matches(rel, isDir) {
			return true
		}
	}
	return false
}

func (p ignorePattern) matches(rel string, isDir bool) bool {
	if p.hasGlob {
		if ok, _ := path.Match(p.raw, rel); ok {
			return true
		}
		if !p.hasPath {
			for _, segment := range strings.Split(rel, "/") {
				if ok, _ := path.Match(p.raw, segment); ok {
					return true
				}
			}
		}
		return false
	}

	if p.hasPath {
		return rel == p.raw || strings.HasPrefix(rel, p.raw+"/")
	}

	for _, segment := range strings.Split(rel, "/") {
		if segment == p.raw {
			return true
		}
	}
	return isDir && path.Base(rel) == p.raw
}

func normalizeIgnoreEntry(entry string) string {
	entry = strings.TrimSpace(filepath.ToSlash(entry))
	entry = strings.Trim(entry, "/")
	if entry == "" {
		return ""
	}
	return path.Clean(entry)
}

func validateIgnorePattern(pattern string) error {
	if !strings.ContainsAny(pattern, "*?[\\") {
		return nil
	}
	_, err := path.Match(pattern, "")
	return err
}

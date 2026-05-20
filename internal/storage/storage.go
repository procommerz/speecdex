package storage

import (
	"encoding/gob"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/procommerz/speecdex-search/internal/indexing"
)

const (
	FormatName             = "speecdex-index"
	FormatVersion          = 2
	DefaultArtifactRelPath = ".speecdex/index.bin"
	DistanceMetricCosine   = "cosine"
	speecdexDataDirName    = ".speecdex"
	temporaryArtifactGlob  = "index-*.tmp"
)

type Index struct {
	Header  Header
	Sources []SourceFile
	Chunks  []Chunk
}

type Header struct {
	FormatName             string
	FormatVersion          int
	CreatedAt              time.Time
	ProjectRootIdentity    string
	EmbeddingProviderStyle string
	EmbeddingModelIdentity string
	EmbeddingDimensions    int
	DistanceMetric         string
	ChunkSize              int
	ChunkOverlap           int
	OnlyEntries            []string
	IgnoredEntries         []string
}

type SourceFile struct {
	RelativePath string
	ContentHash  string
	Size         int64
	ModifiedAt   time.Time
	Deleted      bool
}

type Chunk struct {
	ID          string
	SourcePath  string
	ContentHash string
	StartLine   int
	EndLine     int
	Text        string
	Vector      []float64
	Deleted     bool
}

type ReadOptions struct {
	Compatibility CompatibilityOptions
}

type CompatibilityOptions struct {
	EmbeddingModelIdentity string
	EmbeddingDimensions    int
	DistanceMetric         string
}

type BuildOptions struct {
	CreatedAt              time.Time
	ProjectRoot            string
	EmbeddingProviderStyle string
	EmbeddingModelIdentity string
	EmbeddingDimensions    int
	DistanceMetric         string
	ChunkSize              int
	ChunkOverlap           int
	OnlyEntries            []string
	IgnoredEntries         []string
}

type Error struct {
	Path    string
	Message string
	Err     error
}

func (e *Error) Error() string {
	if e.Path == "" {
		if e.Err == nil {
			return e.Message
		}
		return fmt.Sprintf("%s: %v", e.Message, e.Err)
	}
	if e.Err == nil {
		return fmt.Sprintf("%s: %s", e.Path, e.Message)
	}
	return fmt.Sprintf("%s: %s: %v", e.Path, e.Message, e.Err)
}

func (e *Error) Unwrap() error {
	return e.Err
}

func ArtifactPath(projectRoot string) string {
	return filepath.Join(projectRoot, filepath.FromSlash(DefaultArtifactRelPath))
}

func BuildIndex(opts BuildOptions, files []indexing.MarkdownFile, chunks []indexing.Chunk, vectors [][]float64) (Index, error) {
	if len(chunks) != len(vectors) {
		return Index{}, &Error{Message: fmt.Sprintf("build index: got %d chunks and %d vectors", len(chunks), len(vectors))}
	}

	sources := make([]SourceFile, len(files))
	sourceHashes := make(map[string]string, len(files))
	for i, file := range files {
		sources[i] = SourceFile{
			RelativePath: file.RelativePath,
			ContentHash:  file.ContentHash,
			Size:         file.Size,
			ModifiedAt:   file.ModifiedAt,
		}
		sourceHashes[file.RelativePath] = file.ContentHash
	}
	storedChunks := make([]Chunk, len(chunks))
	for i, chunk := range chunks {
		vector := append([]float64(nil), vectors[i]...)
		contentHash, ok := sourceHashes[chunk.SourcePath]
		if !ok {
			return Index{}, &Error{Message: fmt.Sprintf("build index: chunk %s source %q is missing", chunk.ID, chunk.SourcePath)}
		}
		storedChunks[i] = Chunk{
			ID:          chunk.ID,
			SourcePath:  chunk.SourcePath,
			ContentHash: contentHash,
			StartLine:   chunk.StartLine,
			EndLine:     chunk.EndLine,
			Text:        chunk.Text,
			Vector:      vector,
		}
	}

	return BuildIndexFromRecords(opts, sources, storedChunks)
}

func BuildIndexFromRecords(opts BuildOptions, sources []SourceFile, chunks []Chunk) (Index, error) {
	header, err := newHeader(opts)
	if err != nil {
		return Index{}, err
	}

	idx := Index{
		Header:  header,
		Sources: cloneSources(sources),
		Chunks:  cloneChunks(chunks),
	}
	if err := validateChunkVectors(idx); err != nil {
		return Index{}, err
	}
	return idx, nil
}

func Write(projectRoot string, idx Index) error {
	artifactPath := ArtifactPath(projectRoot)
	dataDir := filepath.Dir(artifactPath)
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return &Error{Path: dataDir, Message: "create Speecdex data directory", Err: err}
	}

	prepared, err := prepareForWrite(projectRoot, idx)
	if err != nil {
		return err
	}

	tmp, err := createTempFile(dataDir, temporaryArtifactGlob)
	if err != nil {
		return &Error{Path: dataDir, Message: "create temporary index artifact", Err: err}
	}
	tmpPath := tmp.Name()
	removeTemp := true
	defer func() {
		if removeTemp {
			_ = os.Remove(tmpPath)
		}
	}()

	if err := gob.NewEncoder(tmp).Encode(prepared); err != nil {
		_ = tmp.Close()
		return &Error{Path: tmpPath, Message: "encode index artifact", Err: err}
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return &Error{Path: tmpPath, Message: "flush index artifact", Err: err}
	}
	if err := tmp.Close(); err != nil {
		return &Error{Path: tmpPath, Message: "close index artifact", Err: err}
	}

	if err := os.Rename(tmpPath, artifactPath); err != nil {
		return &Error{Path: artifactPath, Message: "replace index artifact", Err: err}
	}
	removeTemp = false
	_ = syncDirectory(dataDir)
	return nil
}

func Read(projectRoot string, opts ReadOptions) (Index, error) {
	artifactPath := ArtifactPath(projectRoot)
	file, err := os.Open(artifactPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Index{}, &Error{Path: artifactPath, Message: "index artifact is missing"}
		}
		return Index{}, &Error{Path: artifactPath, Message: "open index artifact", Err: err}
	}
	defer file.Close()

	var idx Index
	if err := gob.NewDecoder(file).Decode(&idx); err != nil {
		return Index{}, &Error{Path: artifactPath, Message: "decode index artifact", Err: err}
	}
	if err := validateHeader(idx.Header); err != nil {
		return Index{}, err
	}
	if err := validateChunkVectors(idx); err != nil {
		return Index{}, err
	}
	if err := ValidateCompatibility(idx.Header, opts.Compatibility); err != nil {
		return Index{}, err
	}
	return idx, nil
}

func ValidateCompatibility(header Header, opts CompatibilityOptions) error {
	if err := validateHeader(header); err != nil {
		return err
	}
	if opts.EmbeddingDimensions > 0 && header.EmbeddingDimensions != opts.EmbeddingDimensions {
		return &Error{Message: fmt.Sprintf("index embedding dimensions %d are incompatible with active dimensions %d", header.EmbeddingDimensions, opts.EmbeddingDimensions)}
	}
	if opts.EmbeddingModelIdentity != "" && header.EmbeddingModelIdentity != opts.EmbeddingModelIdentity {
		return &Error{Message: fmt.Sprintf("index embedding model %q is incompatible with active model %q", header.EmbeddingModelIdentity, opts.EmbeddingModelIdentity)}
	}
	if opts.DistanceMetric != "" && header.DistanceMetric != opts.DistanceMetric {
		return &Error{Message: fmt.Sprintf("index distance metric %q is incompatible with active metric %q", header.DistanceMetric, opts.DistanceMetric)}
	}
	return nil
}

func IsRebuildCompatible(header Header, opts BuildOptions) (bool, error) {
	if err := validateHeader(header); err != nil {
		return false, err
	}
	identity, err := projectRootIdentity(opts.ProjectRoot)
	if err != nil {
		return false, err
	}
	distanceMetric := opts.DistanceMetric
	if distanceMetric == "" {
		distanceMetric = DistanceMetricCosine
	}
	return header.ProjectRootIdentity == identity &&
		header.EmbeddingProviderStyle == opts.EmbeddingProviderStyle &&
		header.EmbeddingModelIdentity == opts.EmbeddingModelIdentity &&
		header.EmbeddingDimensions == opts.EmbeddingDimensions &&
		header.DistanceMetric == distanceMetric &&
		header.ChunkSize == opts.ChunkSize &&
		header.ChunkOverlap == opts.ChunkOverlap &&
		stringSlicesEqual(header.OnlyEntries, normalizedEntries(opts.OnlyEntries)) &&
		stringSlicesEqual(header.IgnoredEntries, normalizedEntries(opts.IgnoredEntries)), nil
}

func prepareForWrite(projectRoot string, idx Index) (Index, error) {
	header := idx.Header
	if header.FormatName == "" {
		header.FormatName = FormatName
	}
	if header.FormatVersion == 0 {
		header.FormatVersion = FormatVersion
	}
	if header.CreatedAt.IsZero() {
		header.CreatedAt = time.Now().UTC()
	}
	if header.ProjectRootIdentity == "" {
		identity, err := projectRootIdentity(projectRoot)
		if err != nil {
			return Index{}, err
		}
		header.ProjectRootIdentity = identity
	}
	if header.DistanceMetric == "" {
		header.DistanceMetric = DistanceMetricCosine
	}
	header.OnlyEntries = normalizedEntries(header.OnlyEntries)
	header.IgnoredEntries = normalizedEntries(header.IgnoredEntries)
	idx.Header = header
	if err := validateHeader(idx.Header); err != nil {
		return Index{}, err
	}
	if err := validateChunkVectors(idx); err != nil {
		return Index{}, err
	}
	return idx, nil
}

func newHeader(opts BuildOptions) (Header, error) {
	createdAt := opts.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	identity, err := projectRootIdentity(opts.ProjectRoot)
	if err != nil {
		return Header{}, err
	}
	distanceMetric := opts.DistanceMetric
	if distanceMetric == "" {
		distanceMetric = DistanceMetricCosine
	}
	header := Header{
		FormatName:             FormatName,
		FormatVersion:          FormatVersion,
		CreatedAt:              createdAt,
		ProjectRootIdentity:    identity,
		EmbeddingProviderStyle: opts.EmbeddingProviderStyle,
		EmbeddingModelIdentity: opts.EmbeddingModelIdentity,
		EmbeddingDimensions:    opts.EmbeddingDimensions,
		DistanceMetric:         distanceMetric,
		ChunkSize:              opts.ChunkSize,
		ChunkOverlap:           opts.ChunkOverlap,
		OnlyEntries:            normalizedEntries(opts.OnlyEntries),
		IgnoredEntries:         normalizedEntries(opts.IgnoredEntries),
	}
	if err := validateHeader(header); err != nil {
		return Header{}, err
	}
	return header, nil
}

func validateHeader(header Header) error {
	if header.FormatName != FormatName {
		return &Error{Message: fmt.Sprintf("unsupported index format %q", header.FormatName)}
	}
	if header.FormatVersion != FormatVersion {
		return &Error{Message: fmt.Sprintf("unsupported index format version %d", header.FormatVersion)}
	}
	if header.DistanceMetric != DistanceMetricCosine {
		return &Error{Message: fmt.Sprintf("unsupported index distance metric %q", header.DistanceMetric)}
	}
	if header.EmbeddingDimensions <= 0 {
		return &Error{Message: "index embedding dimensions must be greater than zero"}
	}
	return nil
}

func validateChunkVectors(idx Index) error {
	for _, chunk := range idx.Chunks {
		if len(chunk.Vector) != idx.Header.EmbeddingDimensions {
			return &Error{
				Message: fmt.Sprintf("chunk %s vector has %d dimensions, want %d", chunk.ID, len(chunk.Vector), idx.Header.EmbeddingDimensions),
			}
		}
	}
	return nil
}

func projectRootIdentity(projectRoot string) (string, error) {
	root := projectRoot
	if root == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", &Error{Message: "determine project root", Err: err}
		}
		root = wd
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", &Error{Path: root, Message: "resolve project root", Err: err}
	}
	return filepath.Clean(absRoot), nil
}

func normalizedEntries(entries []string) []string {
	normalized := append([]string(nil), entries...)
	sort.Strings(normalized)
	return normalized
}

func cloneSources(sources []SourceFile) []SourceFile {
	return append([]SourceFile(nil), sources...)
}

func cloneChunks(chunks []Chunk) []Chunk {
	out := make([]Chunk, len(chunks))
	for i, chunk := range chunks {
		out[i] = chunk
		out[i].Vector = append([]float64(nil), chunk.Vector...)
	}
	return out
}

func stringSlicesEqual(a []string, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

var createTempFile = os.CreateTemp

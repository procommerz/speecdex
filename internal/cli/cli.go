package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/procommerz/speecdex-search/internal/config"
	"github.com/procommerz/speecdex-search/internal/embeddings"
	"github.com/procommerz/speecdex-search/internal/indexing"
	"github.com/procommerz/speecdex-search/internal/search"
	"github.com/procommerz/speecdex-search/internal/storage"
)

const (
	ExitOK           = 0
	ExitRuntimeError = 1
	ExitUsageError   = 2
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

type Mode string

const (
	ModeIndex        Mode = "index"
	ModeSearch       Mode = "search"
	ModeService      Mode = "service"
	ModeShowBranch   Mode = "show-branch"
	ModeInit         Mode = "init"
	ModeInstallSkill Mode = "install-skill"
	ModeVersion      Mode = "version"
)

type Options struct {
	Mode         Mode
	Query        string
	Text         []string
	Service      bool
	Force        bool
	ShowBranch   bool
	Init         bool
	InstallSkill bool
	Version      bool
}

type textFlags []string

var detectGitBranch = currentGitBranch

func (t *textFlags) String() string {
	return strings.Join(*t, ",")
}

func (t *textFlags) Set(value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("--text requires a non-empty value")
	}

	*t = append(*t, value)
	return nil
}

func Run(args []string, stdout io.Writer, stderr io.Writer) int {
	return run(args, stdout, stderr, config.LoadOptions{})
}

func run(args []string, stdout io.Writer, stderr io.Writer, loadOptions config.LoadOptions) int {
	return runWithClock(args, stdout, stderr, loadOptions, time.Now)
}

func runWithClock(args []string, stdout io.Writer, stderr io.Writer, loadOptions config.LoadOptions, now func() time.Time) int {
	opts, err := Parse(args, stderr)
	if err != nil {
		return ExitUsageError
	}

	if opts.Mode == ModeVersion {
		return runVersion(stdout)
	}
	if loadOptions.ProjectRoot == "" {
		projectRoot, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(stderr, "%v\n", err)
			return ExitRuntimeError
		}
		loadOptions.ProjectRoot = projectRoot
	}
	if opts.Mode == ModeShowBranch {
		return runShowBranch(loadOptions.ProjectRoot, stdout, stderr)
	}
	if opts.Mode == ModeInit {
		return runInit(loadOptions.ProjectRoot, stdout, stderr)
	}
	if opts.Mode == ModeInstallSkill {
		return runInstallSkill(loadOptions.ProjectRoot, stdout, stderr)
	}
	if loadOptions.UserHome == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintf(stderr, "%v\n", err)
			return ExitRuntimeError
		}
		loadOptions.UserHome = userHome
	}
	cfg, err := config.Load(loadOptions)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return ExitRuntimeError
	}

	switch opts.Mode {
	case ModeIndex:
		return runIndex(opts, cfg, loadOptions.ProjectRoot, stdout, stderr, now)
	case ModeSearch:
		return runSearch(opts, cfg, loadOptions.ProjectRoot, stdout, stderr)
	case ModeService:
		fmt.Fprintf(stderr, "speecdex %s mode is not implemented yet\n", opts.Mode)
		return ExitRuntimeError
	default:
		fmt.Fprintf(stderr, "speecdex %s mode is not implemented yet\n", opts.Mode)
		return ExitRuntimeError
	}
}

func runIndex(opts Options, cfg config.Config, projectRoot string, stdout io.Writer, stderr io.Writer, now func() time.Time) int {
	if cfg.Embedding == nil {
		fmt.Fprintln(stderr, "indexing requires llms.embedding configuration")
		return ExitRuntimeError
	}

	absRoot, err := filepath.Abs(projectRoot)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return ExitRuntimeError
	}

	discovery, err := indexing.DiscoverMarkdown(indexing.Options{
		ProjectRoot:            absRoot,
		OnlyEntries:            cfg.OnlyEntries,
		IgnoredEntries:         cfg.IgnoredEntries,
		ChunkSize:              cfg.Indexing.ChunkSize,
		ChunkOverlap:           cfg.Indexing.ChunkOverlap,
		EmbeddingModelIdentity: cfg.Embedding.ModelName,
	})
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return ExitRuntimeError
	}

	chunkOptions := indexing.Options{
		ChunkSize:              cfg.Indexing.ChunkSize,
		ChunkOverlap:           cfg.Indexing.ChunkOverlap,
		EmbeddingModelIdentity: cfg.Embedding.ModelName,
	}
	buildOptions := storage.BuildOptions{
		ProjectRoot:            absRoot,
		EmbeddingProviderStyle: cfg.Embedding.Style,
		EmbeddingModelIdentity: cfg.Embedding.ModelName,
		EmbeddingDimensions:    cfg.Embedding.DefaultDims,
		DistanceMetric:         storage.DistanceMetricCosine,
		ChunkSize:              cfg.Indexing.ChunkSize,
		ChunkOverlap:           cfg.Indexing.ChunkOverlap,
		OnlyEntries:            cfg.OnlyEntries,
		IgnoredEntries:         cfg.IgnoredEntries,
		GitBranch:              detectGitBranch(absRoot),
	}
	previous, hasPrevious := reusablePreviousIndex(absRoot, buildOptions, stderr)
	plan := planIndexRebuild(discovery.Files, chunkOptions, previous, hasPrevious, opts.Force)

	var embedder search.Embedder
	progress := newIndexProgress(stderr, len(plan.Files), plan.ActiveChunkCount(), now)
	chunks := make([]storage.Chunk, 0, plan.ActiveChunkCount()+len(plan.DeletedChunks))
	indexedChunks := 0
	for index, filePlan := range plan.Files {
		fileChunks := filePlan.ReusedChunks
		if len(filePlan.ChunksToEmbed) > 0 {
			if embedder == nil {
				embedder, err = newEmbedder(cfg)
				if err != nil {
					fmt.Fprintf(stderr, "%v\n", err)
					return ExitRuntimeError
				}
			}
			texts := make([]string, len(filePlan.ChunksToEmbed))
			for i, chunk := range filePlan.ChunksToEmbed {
				texts[i] = chunk.Text
			}
			fileVectors, err := embedder.Embed(context.Background(), texts)
			if err != nil {
				fmt.Fprintf(stderr, "embed chunks: %v\n", err)
				return ExitRuntimeError
			}
			embedded, err := storageChunksForFile(filePlan.File, filePlan.ChunksToEmbed, fileVectors)
			if err != nil {
				fmt.Fprintf(stderr, "%v\n", err)
				return ExitRuntimeError
			}
			fileChunks = embedded
		}

		chunks = append(chunks, fileChunks...)
		indexedChunks += len(fileChunks)
		progress.Report(index+1, filePlan.File.RelativePath, len(fileChunks), meanChunkTextCharacters(fileChunks), indexedChunks)
	}
	chunks = append(chunks, plan.DeletedChunks...)
	sortStorageChunks(chunks)

	idx, err := storage.BuildIndexFromRecords(buildOptions, plan.Sources, chunks)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return ExitRuntimeError
	}

	if err := storage.Write(absRoot, idx); err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return ExitRuntimeError
	}

	fmt.Fprintf(stderr, "Index rebuild: reused %d chunks | embedded %d chunks | retained deleted %d chunks\n", plan.ReusedChunks, plan.EmbeddedChunks, plan.RetainedDeletedChunks)
	fmt.Fprintf(stdout, "Project root: %s\n", absRoot)
	fmt.Fprintf(stdout, "Markdown files discovered: %d\n", len(discovery.Files))
	fmt.Fprintf(stdout, "Markdown files indexed: %d\n", len(discovery.Files))
	fmt.Fprintf(stdout, "Chunks indexed: %d\n", plan.ActiveChunkCount())
	fmt.Fprintf(stdout, "Ignored entries: %d\n", discovery.IgnoredEntriesCount)
	fmt.Fprintf(stdout, "Embedding provider: %s/%s\n", cfg.Embedding.Style, cfg.Embedding.ModelName)
	fmt.Fprintf(stdout, "Index artifact: %s\n", storage.ArtifactPath(absRoot))
	if idx.Header.GitBranch != "" {
		fmt.Fprintf(stdout, "Indexed git branch: %s\n", idx.Header.GitBranch)
	}
	return ExitOK
}

type rebuildPlan struct {
	Files                 []rebuildFilePlan
	Sources               []storage.SourceFile
	DeletedChunks         []storage.Chunk
	ReusedChunks          int
	EmbeddedChunks        int
	RetainedDeletedChunks int
}

type rebuildFilePlan struct {
	File          indexing.MarkdownFile
	ReusedChunks  []storage.Chunk
	ChunksToEmbed []indexing.Chunk
}

func (p rebuildPlan) ActiveChunkCount() int {
	return p.ReusedChunks + p.EmbeddedChunks
}

func reusablePreviousIndex(projectRoot string, opts storage.BuildOptions, stderr io.Writer) (storage.Index, bool) {
	artifactPath := storage.ArtifactPath(projectRoot)
	if _, err := os.Stat(artifactPath); err != nil {
		return storage.Index{}, false
	}

	previous, err := storage.Read(projectRoot, storage.ReadOptions{})
	if err != nil {
		fmt.Fprintf(stderr, "Index rebuild: previous index cannot be reused: %v\n", err)
		return storage.Index{}, false
	}
	ok, err := storage.IsRebuildCompatible(previous.Header, opts)
	if err != nil {
		fmt.Fprintf(stderr, "Index rebuild: previous index cannot be reused: %v\n", err)
		return storage.Index{}, false
	}
	if !ok {
		fmt.Fprintln(stderr, "Index rebuild: previous index cannot be reused: not compatible with active indexing configuration")
		return storage.Index{}, false
	}
	return previous, true
}

func planIndexRebuild(files []indexing.MarkdownFile, opts indexing.Options, previous storage.Index, hasPrevious bool, force bool) rebuildPlan {
	previousSources := map[string]storage.SourceFile{}
	previousChunks := map[string][]storage.Chunk{}
	if hasPrevious {
		for _, source := range previous.Sources {
			previousSources[source.RelativePath] = source
		}
		for _, chunk := range previous.Chunks {
			previousChunks[chunk.SourcePath] = append(previousChunks[chunk.SourcePath], chunk)
		}
	}

	seen := map[string]bool{}
	plan := rebuildPlan{
		Files:   make([]rebuildFilePlan, 0, len(files)),
		Sources: make([]storage.SourceFile, 0, len(files)+len(previousSources)),
	}
	for _, file := range files {
		seen[file.RelativePath] = true
		source := storage.SourceFile{
			RelativePath: file.RelativePath,
			ContentHash:  file.ContentHash,
			Size:         file.Size,
			ModifiedAt:   file.ModifiedAt,
		}
		plan.Sources = append(plan.Sources, source)

		previousSource, canReuse := previousSources[file.RelativePath]
		reusableChunks := previousChunks[file.RelativePath]
		canReuse = canReuse && !force && previousSource.ContentHash == file.ContentHash && len(reusableChunks) > 0
		if canReuse {
			reused := cloneStorageChunks(reusableChunks)
			for i := range reused {
				reused[i].ContentHash = file.ContentHash
				reused[i].Deleted = false
			}
			sortStorageChunks(reused)
			plan.Files = append(plan.Files, rebuildFilePlan{File: file, ReusedChunks: reused})
			plan.ReusedChunks += len(reused)
			continue
		}

		chunks := indexing.ChunkMarkdown(file, opts)
		plan.Files = append(plan.Files, rebuildFilePlan{File: file, ChunksToEmbed: chunks})
		plan.EmbeddedChunks += len(chunks)
	}

	if hasPrevious {
		for _, source := range previous.Sources {
			if seen[source.RelativePath] {
				continue
			}
			source.Deleted = true
			plan.Sources = append(plan.Sources, source)
			deletedChunks := cloneStorageChunks(previousChunks[source.RelativePath])
			for i := range deletedChunks {
				deletedChunks[i].Deleted = true
			}
			plan.RetainedDeletedChunks += len(deletedChunks)
			plan.DeletedChunks = append(plan.DeletedChunks, deletedChunks...)
		}
	}

	sortStorageSources(plan.Sources)
	return plan
}

func storageChunksForFile(file indexing.MarkdownFile, chunks []indexing.Chunk, vectors [][]float64) ([]storage.Chunk, error) {
	if len(chunks) != len(vectors) {
		return nil, fmt.Errorf("build index: got %d chunks and %d vectors", len(chunks), len(vectors))
	}
	out := make([]storage.Chunk, len(chunks))
	for i, chunk := range chunks {
		out[i] = storage.Chunk{
			ID:          chunk.ID,
			SourcePath:  chunk.SourcePath,
			ContentHash: file.ContentHash,
			StartLine:   chunk.StartLine,
			EndLine:     chunk.EndLine,
			Text:        chunk.Text,
			Vector:      append([]float64(nil), vectors[i]...),
		}
	}
	return out, nil
}

func meanChunkTextCharacters(chunks []storage.Chunk) int {
	if len(chunks) == 0 {
		return 0
	}
	total := 0
	for _, chunk := range chunks {
		total += len([]rune(chunk.Text))
	}
	return int(float64(total)/float64(len(chunks)) + 0.5)
}

func cloneStorageChunks(chunks []storage.Chunk) []storage.Chunk {
	out := make([]storage.Chunk, len(chunks))
	for i, chunk := range chunks {
		out[i] = chunk
		out[i].Vector = append([]float64(nil), chunk.Vector...)
	}
	return out
}

func sortStorageSources(sources []storage.SourceFile) {
	sort.SliceStable(sources, func(i, j int) bool {
		return sources[i].RelativePath < sources[j].RelativePath
	})
}

func sortStorageChunks(chunks []storage.Chunk) {
	sort.SliceStable(chunks, func(i, j int) bool {
		if chunks[i].SourcePath != chunks[j].SourcePath {
			return chunks[i].SourcePath < chunks[j].SourcePath
		}
		if chunks[i].StartLine != chunks[j].StartLine {
			return chunks[i].StartLine < chunks[j].StartLine
		}
		if chunks[i].EndLine != chunks[j].EndLine {
			return chunks[i].EndLine < chunks[j].EndLine
		}
		return chunks[i].ID < chunks[j].ID
	})
}

type indexProgress struct {
	writer      io.Writer
	totalFiles  int
	totalChunks int
	startedAt   time.Time
	now         func() time.Time
}

func newIndexProgress(writer io.Writer, totalFiles int, totalChunks int, now func() time.Time) indexProgress {
	if now == nil {
		now = time.Now
	}
	return indexProgress{
		writer:      writer,
		totalFiles:  totalFiles,
		totalChunks: totalChunks,
		startedAt:   now(),
		now:         now,
	}
}

func (p indexProgress) Report(currentFile int, path string, fileChunks int, meanChunkChars int, indexedChunks int) {
	elapsed := p.now().Sub(p.startedAt)
	if elapsed < 0 {
		elapsed = 0
	}

	rate := 0.0
	eta := time.Duration(0)
	if indexedChunks > 0 && elapsed > 0 {
		rate = float64(indexedChunks) / elapsed.Seconds()
		remainingChunks := p.totalChunks - indexedChunks
		if remainingChunks > 0 {
			eta = time.Duration(float64(remainingChunks)/rate*float64(time.Second) + 0.5)
		}
	}

	fmt.Fprintf(
		p.writer,
		"Indexing file %d/%d: %s (%d chunks, mean %d chars) | elapsed %s | %.2f chunks/s | ETA %s\n",
		currentFile,
		p.totalFiles,
		path,
		fileChunks,
		meanChunkChars,
		elapsed.Round(time.Second),
		rate,
		eta.Round(time.Second),
	)
}

func runSearch(opts Options, cfg config.Config, projectRoot string, stdout io.Writer, stderr io.Writer) int {
	var embedder search.Embedder
	readOptions := storage.ReadOptions{}
	if opts.Query != "" {
		if cfg.Embedding == nil {
			fmt.Fprintln(stderr, "semantic search requires llms.embedding configuration")
			return ExitRuntimeError
		}
		readOptions.Compatibility = storage.CompatibilityOptions{
			EmbeddingModelIdentity: cfg.Embedding.ModelName,
			EmbeddingDimensions:    cfg.Embedding.DefaultDims,
			DistanceMetric:         storage.DistanceMetricCosine,
		}
	}

	idx, err := storage.Read(projectRoot, readOptions)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return ExitRuntimeError
	}

	if opts.Query != "" {
		embedder, err = newEmbedder(cfg)
		if err != nil {
			fmt.Fprintf(stderr, "%v\n", err)
			return ExitRuntimeError
		}
	}

	results, err := search.Run(context.Background(), idx, search.Options{
		Query: opts.Query,
		Text:  opts.Text,
	}, embedder)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return ExitRuntimeError
	}
	if err := search.WriteYAML(stdout, results, idx.Header.GitBranch); err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return ExitRuntimeError
	}
	return ExitOK
}

func runShowBranch(projectRoot string, stdout io.Writer, stderr io.Writer) int {
	idx, err := storage.Read(projectRoot, storage.ReadOptions{})
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return ExitRuntimeError
	}
	if idx.Header.GitBranch != "" {
		fmt.Fprintln(stdout, idx.Header.GitBranch)
	}
	return ExitOK
}

func runVersion(stdout io.Writer) int {
	fmt.Fprintf(stdout, "speecdex %s (commit %s, built %s)\n", version, commit, date)
	return ExitOK
}

func currentGitBranch(projectRoot string) string {
	out, err := exec.Command("git", "-C", projectRoot, "branch", "--show-current").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func newEmbedder(cfg config.Config) (search.Embedder, error) {
	model := cfg.Embedding
	endpoint := model.Endpoint
	switch model.Style {
	case config.StyleOpenAICompatible:
	case config.StyleGGUF:
		endpoint = fmt.Sprintf("http://127.0.0.1:%d/v1", cfg.Service.Port)
	default:
		return nil, fmt.Errorf("unsupported embedding provider style %q", model.Style)
	}

	return embeddings.NewOpenAICompatibleClient(embeddings.OpenAICompatibleOptions{
		Endpoint:   endpoint,
		ModelName:  model.ModelName,
		APIKey:     model.APIKey,
		Dimensions: model.DefaultDims,
	})
}

func Parse(args []string, stderr io.Writer) (Options, error) {
	var opts Options
	var texts textFlags

	flags := flag.NewFlagSet("speecdex", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage:")
		fmt.Fprintln(stderr, "  speecdex [--force]")
		fmt.Fprintln(stderr, "  speecdex --query <query> [--text <literal> ...]")
		fmt.Fprintln(stderr, "  speecdex --text <literal> [--text <literal> ...]")
		fmt.Fprintln(stderr, "  speecdex --show-branch")
		fmt.Fprintln(stderr, "  speecdex --service")
		fmt.Fprintln(stderr, "  speecdex --init")
		fmt.Fprintln(stderr, "  speecdex --install-skill")
		fmt.Fprintln(stderr, "  speecdex --version")
	}

	flags.StringVar(&opts.Query, "query", "", "semantic search query")
	flags.Var(&texts, "text", "literal text search term; may be repeated")
	flags.BoolVar(&opts.Service, "service", false, "start the local embedding service")
	flags.BoolVar(&opts.Force, "force", false, "rebuild current files without reusing checksum-matched chunks")
	flags.BoolVar(&opts.ShowBranch, "show-branch", false, "print the git branch stored in the local index")
	flags.BoolVar(&opts.Init, "init", false, "initialize project configuration files")
	flags.BoolVar(&opts.InstallSkill, "install-skill", false, "install the docs-search skill into local project agent folders")
	flags.BoolVar(&opts.Version, "version", false, "print version information")

	if err := flags.Parse(args); err != nil {
		return Options{}, err
	}

	if flags.NArg() > 0 {
		return Options{}, usageError(stderr, flags, "unexpected positional arguments")
	}

	opts.Query = strings.TrimSpace(opts.Query)
	opts.Text = append([]string(nil), texts...)

	if opts.Query == "" && hasFlag(args, "--query") {
		return Options{}, usageError(stderr, flags, "--query requires a non-empty value")
	}

	if opts.Init && (opts.ShowBranch || opts.Service || opts.Force || opts.Query != "" || len(opts.Text) > 0) {
		return Options{}, usageError(stderr, flags, "--init cannot be combined with other flags")
	}
	if opts.InstallSkill && (opts.Init || opts.ShowBranch || opts.Service || opts.Force || opts.Query != "" || len(opts.Text) > 0) {
		return Options{}, usageError(stderr, flags, "--install-skill cannot be combined with other flags")
	}
	if opts.Version && (opts.Init || opts.InstallSkill || opts.ShowBranch || opts.Service || opts.Force || opts.Query != "" || len(opts.Text) > 0) {
		return Options{}, usageError(stderr, flags, "--version cannot be combined with other flags")
	}
	if opts.ShowBranch && (opts.Service || opts.Force || opts.Query != "" || len(opts.Text) > 0) {
		return Options{}, usageError(stderr, flags, "--show-branch cannot be combined with other flags")
	}
	if opts.Service && (opts.Query != "" || len(opts.Text) > 0) {
		return Options{}, usageError(stderr, flags, "--service cannot be combined with --query or --text")
	}
	if opts.Force && (opts.Service || opts.Query != "" || len(opts.Text) > 0) {
		return Options{}, usageError(stderr, flags, "--force can only be used while indexing")
	}

	switch {
	case opts.Init:
		opts.Mode = ModeInit
	case opts.InstallSkill:
		opts.Mode = ModeInstallSkill
	case opts.Version:
		opts.Mode = ModeVersion
	case opts.ShowBranch:
		opts.Mode = ModeShowBranch
	case opts.Service:
		opts.Mode = ModeService
	case opts.Query != "" || len(opts.Text) > 0:
		opts.Mode = ModeSearch
	default:
		opts.Mode = ModeIndex
	}

	return opts, nil
}

func usageError(stderr io.Writer, flags *flag.FlagSet, message string) error {
	fmt.Fprintf(stderr, "error: %s\n", message)
	flags.Usage()
	return errors.New(message)
}

func hasFlag(args []string, name string) bool {
	for _, arg := range args {
		if arg == name || strings.HasPrefix(arg, name+"=") {
			return true
		}
	}

	return false
}

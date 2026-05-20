package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/procommerz/speecdex-search/internal/config"
	"github.com/procommerz/speecdex-search/internal/embeddings"
	"github.com/procommerz/speecdex-search/internal/search"
	"github.com/procommerz/speecdex-search/internal/storage"
)

const (
	ExitOK           = 0
	ExitRuntimeError = 1
	ExitUsageError   = 2
)

type Mode string

const (
	ModeIndex   Mode = "index"
	ModeSearch  Mode = "search"
	ModeService Mode = "service"
)

type Options struct {
	Mode    Mode
	Query   string
	Text    []string
	Service bool
}

type textFlags []string

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
	opts, err := Parse(args, stderr)
	if err != nil {
		return ExitUsageError
	}

	if loadOptions.ProjectRoot == "" {
		projectRoot, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(stderr, "%v\n", err)
			return ExitRuntimeError
		}
		loadOptions.ProjectRoot = projectRoot
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

	if opts.Mode == ModeSearch {
		return runSearch(opts, cfg, loadOptions.ProjectRoot, stdout, stderr)
	}

	fmt.Fprintf(stderr, "speecdex %s mode is not implemented yet\n", opts.Mode)
	return ExitRuntimeError
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
		embedder, err = newSearchEmbedder(cfg)
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
	if err := search.WriteYAML(stdout, results); err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return ExitRuntimeError
	}
	return ExitOK
}

func newSearchEmbedder(cfg config.Config) (search.Embedder, error) {
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
		fmt.Fprintln(stderr, "  speecdex")
		fmt.Fprintln(stderr, "  speecdex --query <query> [--text <literal> ...]")
		fmt.Fprintln(stderr, "  speecdex --text <literal> [--text <literal> ...]")
		fmt.Fprintln(stderr, "  speecdex --service")
	}

	flags.StringVar(&opts.Query, "query", "", "semantic search query")
	flags.Var(&texts, "text", "literal text search term; may be repeated")
	flags.BoolVar(&opts.Service, "service", false, "start the local embedding service")

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

	if opts.Service && (opts.Query != "" || len(opts.Text) > 0) {
		return Options{}, usageError(stderr, flags, "--service cannot be combined with --query or --text")
	}

	switch {
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

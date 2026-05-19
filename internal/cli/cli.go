package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
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
	opts, err := Parse(args, stderr)
	if err != nil {
		return ExitUsageError
	}

	_ = stdout
	fmt.Fprintf(stderr, "speecdex %s mode is not implemented yet\n", opts.Mode)
	return ExitRuntimeError
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

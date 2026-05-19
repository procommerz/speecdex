package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/procommerz/speecdex-search/internal/config"
)

func TestParseSelectsModes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want Options
	}{
		{
			name: "no flags selects indexing mode",
			args: nil,
			want: Options{Mode: ModeIndex},
		},
		{
			name: "query selects search mode",
			args: []string{"--query", "root"},
			want: Options{Mode: ModeSearch, Query: "root"},
		},
		{
			name: "repeated text preserves all values",
			args: []string{"--text", "extends BusinessObject", "--text", "implements BusinessObject"},
			want: Options{Mode: ModeSearch, Text: []string{"extends BusinessObject", "implements BusinessObject"}},
		},
		{
			name: "query and text select combined search mode",
			args: []string{"--query", "root", "--text", "extends BusinessObject"},
			want: Options{Mode: ModeSearch, Query: "root", Text: []string{"extends BusinessObject"}},
		},
		{
			name: "service selects service mode",
			args: []string{"--service"},
			want: Options{Mode: ModeService, Service: true},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var stderr bytes.Buffer
			got, err := Parse(tt.args, &stderr)
			if err != nil {
				t.Fatalf("Parse() error = %v, stderr = %q", err, stderr.String())
			}

			assertOptions(t, got, tt.want)
			if stderr.Len() != 0 {
				t.Fatalf("Parse() wrote unexpected stderr: %q", stderr.String())
			}
		})
	}
}

func TestRunRejectsInvalidUsageWithExitCodeTwo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		args       []string
		wantStderr string
	}{
		{
			name:       "service rejects query",
			args:       []string{"--service", "--query", "root"},
			wantStderr: "--service cannot be combined",
		},
		{
			name:       "empty query is invalid",
			args:       []string{"--query", "   "},
			wantStderr: "--query requires a non-empty value",
		},
		{
			name:       "empty text is invalid",
			args:       []string{"--text", ""},
			wantStderr: "--text requires a non-empty value",
		},
		{
			name:       "unknown flag is invalid",
			args:       []string{"--unknown"},
			wantStderr: "Usage:",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var stdout bytes.Buffer
			var stderr bytes.Buffer
			code := Run(tt.args, &stdout, &stderr)
			if code != ExitUsageError {
				t.Fatalf("Run() exit code = %d, want %d", code, ExitUsageError)
			}
			if stdout.Len() != 0 {
				t.Fatalf("Run() wrote unexpected stdout: %q", stdout.String())
			}
			if !strings.Contains(stderr.String(), tt.wantStderr) {
				t.Fatalf("Run() stderr = %q, want to contain %q", stderr.String(), tt.wantStderr)
			}
		})
	}
}

func TestRunReturnsRuntimeErrorForValidUnimplementedModes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "index", args: nil, want: "speecdex index mode is not implemented yet"},
		{name: "search", args: []string{"--query", "root"}, want: "speecdex search mode is not implemented yet"},
		{name: "service", args: []string{"--service"}, want: "speecdex service mode is not implemented yet"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var stdout bytes.Buffer
			var stderr bytes.Buffer
			code := runForTest(t, tt.args, &stdout, &stderr, t.TempDir(), t.TempDir())
			if code != ExitRuntimeError {
				t.Fatalf("Run() exit code = %d, want %d", code, ExitRuntimeError)
			}
			if stdout.Len() != 0 {
				t.Fatalf("Run() wrote unexpected stdout: %q", stdout.String())
			}
			if !strings.Contains(stderr.String(), tt.want) {
				t.Fatalf("Run() stderr = %q, want to contain %q", stderr.String(), tt.want)
			}
		})
	}
}

func TestRunReturnsRuntimeErrorForInvalidConfig(t *testing.T) {
	t.Parallel()

	projectRoot := t.TempDir()
	userHome := t.TempDir()
	writeTestConfig(t, projectRoot, ".speecdex/config.yaml", `
config:
  service:
    port: 70000
`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := runForTest(t, nil, &stdout, &stderr, projectRoot, userHome)
	if code != ExitRuntimeError {
		t.Fatalf("Run() exit code = %d, want %d", code, ExitRuntimeError)
	}
	if stdout.Len() != 0 {
		t.Fatalf("Run() wrote unexpected stdout: %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "config.service.port") {
		t.Fatalf("Run() stderr = %q, want config.service.port", stderr.String())
	}
}

func assertOptions(t *testing.T, got Options, want Options) {
	t.Helper()

	if got.Mode != want.Mode {
		t.Fatalf("Mode = %q, want %q", got.Mode, want.Mode)
	}
	if got.Query != want.Query {
		t.Fatalf("Query = %q, want %q", got.Query, want.Query)
	}
	if got.Service != want.Service {
		t.Fatalf("Service = %t, want %t", got.Service, want.Service)
	}
	if len(got.Text) != len(want.Text) {
		t.Fatalf("Text length = %d, want %d; got %#v", len(got.Text), len(want.Text), got.Text)
	}
	for i := range got.Text {
		if got.Text[i] != want.Text[i] {
			t.Fatalf("Text[%d] = %q, want %q", i, got.Text[i], want.Text[i])
		}
	}
}

func runForTest(t *testing.T, args []string, stdout *bytes.Buffer, stderr *bytes.Buffer, projectRoot string, userHome string) int {
	t.Helper()

	return run(args, stdout, stderr, config.LoadOptions{ProjectRoot: projectRoot, UserHome: userHome})
}

func writeTestConfig(t *testing.T, root string, name string, contents string) string {
	t.Helper()

	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte(strings.TrimSpace(contents)+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return path
}

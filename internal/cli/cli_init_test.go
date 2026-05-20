package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunInitCreatesMissingConfigFilesAndDoesNotIndex(t *testing.T) {
	t.Parallel()

	projectRoot := t.TempDir()
	userHome := t.TempDir()
	writeProjectFile(t, projectRoot, "docs/example.md", "# Example\nThis should not be indexed.\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := runForTest(t, []string{"--init"}, &stdout, &stderr, projectRoot, userHome)
	if code != ExitOK {
		t.Fatalf("Run() exit code = %d, want %d; stderr = %q", code, ExitOK, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("Run() wrote unexpected stderr: %q", stderr.String())
	}

	assertFileContents(t, filepath.Join(projectRoot, ".speecdex/config.yaml"), defaultConfigYAML)
	assertFileContents(t, filepath.Join(projectRoot, ".speecdex/llms.yaml"), defaultLLMsYAML)
	assertPathMissing(t, filepath.Join(projectRoot, ".speecdex/index.bin"))

	gotStdout := stdout.String()
	for _, want := range []string{
		"Config directory: " + filepath.Join(projectRoot, ".speecdex"),
		"config: created " + filepath.Join(projectRoot, ".speecdex/config.yaml"),
		"llms: created " + filepath.Join(projectRoot, ".speecdex/llms.yaml"),
	} {
		if !strings.Contains(gotStdout, want) {
			t.Fatalf("stdout = %q, want to contain %q", gotStdout, want)
		}
	}
}

func TestRunInitPreservesExistingYAMLFiles(t *testing.T) {
	t.Parallel()

	projectRoot := t.TempDir()
	userHome := t.TempDir()
	configPath := writeTestConfig(t, projectRoot, ".speecdex/config.yaml", `
config:
  ignored_entries:
    - keep.md
`)
	llmsPath := writeTestConfig(t, projectRoot, ".speecdex/llms.yaml", `
llms:
  embedding:
    style: gguf
    model_name: keep.gguf
    default_dims: 384
`)
	wantConfig := mustReadFile(t, configPath)
	wantLLMs := mustReadFile(t, llmsPath)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := runForTest(t, []string{"--init"}, &stdout, &stderr, projectRoot, userHome)
	if code != ExitOK {
		t.Fatalf("Run() exit code = %d, want %d; stderr = %q", code, ExitOK, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("Run() wrote unexpected stderr: %q", stderr.String())
	}

	assertFileContents(t, configPath, wantConfig)
	assertFileContents(t, llmsPath, wantLLMs)
	if !strings.Contains(stdout.String(), "config: existing "+configPath) ||
		!strings.Contains(stdout.String(), "llms: existing "+llmsPath) {
		t.Fatalf("stdout = %q, want existing statuses", stdout.String())
	}
}

func TestRunInitTreatsExistingYMLAsExistingConfigSlot(t *testing.T) {
	t.Parallel()

	projectRoot := t.TempDir()
	userHome := t.TempDir()
	configYML := writeTestConfig(t, projectRoot, ".speecdex/config.yml", `
config:
  only_entries:
    - notes
`)
	llmsYML := writeTestConfig(t, projectRoot, ".speecdex/llms.yml", `
llms:
  embedding:
    style: gguf
    model_name: keep.gguf
    default_dims: 384
`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := runForTest(t, []string{"--init"}, &stdout, &stderr, projectRoot, userHome)
	if code != ExitOK {
		t.Fatalf("Run() exit code = %d, want %d; stderr = %q", code, ExitOK, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("Run() wrote unexpected stderr: %q", stderr.String())
	}

	assertPathMissing(t, filepath.Join(projectRoot, ".speecdex/config.yaml"))
	assertPathMissing(t, filepath.Join(projectRoot, ".speecdex/llms.yaml"))
	if !strings.Contains(stdout.String(), "config: existing "+configYML) ||
		!strings.Contains(stdout.String(), "llms: existing "+llmsYML) {
		t.Fatalf("stdout = %q, want existing .yml statuses", stdout.String())
	}
}

func TestRunInitCreatesOnlyMissingConfigSlot(t *testing.T) {
	t.Parallel()

	projectRoot := t.TempDir()
	userHome := t.TempDir()
	configPath := writeTestConfig(t, projectRoot, ".speecdex/config.yaml", `
config:
  ignored_entries:
    - keep.md
`)
	wantConfig := mustReadFile(t, configPath)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := runForTest(t, []string{"--init"}, &stdout, &stderr, projectRoot, userHome)
	if code != ExitOK {
		t.Fatalf("Run() exit code = %d, want %d; stderr = %q", code, ExitOK, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("Run() wrote unexpected stderr: %q", stderr.String())
	}

	assertFileContents(t, configPath, wantConfig)
	assertFileContents(t, filepath.Join(projectRoot, ".speecdex/llms.yaml"), defaultLLMsYAML)
	if !strings.Contains(stdout.String(), "config: existing "+configPath) ||
		!strings.Contains(stdout.String(), "llms: created "+filepath.Join(projectRoot, ".speecdex/llms.yaml")) {
		t.Fatalf("stdout = %q, want mixed existing/created statuses", stdout.String())
	}
}

func TestInitTemplatesMatchExampleFiles(t *testing.T) {
	t.Parallel()

	assertFileContents(t, filepath.Join("..", "..", "config.yaml.example"), defaultConfigYAML)
	assertFileContents(t, filepath.Join("..", "..", "llms.yaml.example"), defaultLLMsYAML)
}

func assertFileContents(t *testing.T, path string, want string) {
	t.Helper()

	got := mustReadFile(t, path)
	if got != want {
		t.Fatalf("%s contents = %q, want %q", path, got, want)
	}
}

func mustReadFile(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", path, err)
	}
	return string(data)
}

func assertPathMissing(t *testing.T, path string) {
	t.Helper()

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("Stat(%s) error = %v, want missing path", path, err)
	}
}

package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const defaultConfigYAML = "config:\n" +
	"  only_entries:\n" +
	"    - docs\n" +
	"    - README.md\n" +
	"  ignored_entries:\n" +
	"    - .git\n" +
	"    - ignored.md\n" +
	"    - */plans/*.md"

const defaultLLMsYAML = "llms:\n" +
	"  embedding:\n" +
	"    style: openai-compatible\n" +
	"    endpoint: http://127.0.0.1:1234/v1\n" +
	"    model_name: text-embedding-embeddinggemma-300m-qat\n" +
	"    apiKey:\n" +
	"    default_dims: 768\n" +
	"  # Alternative configuration, for local embedding models:\n" +
	"  # embedding:\n" +
	"  #   style: gguf\n" +
	"  #   model_name: https://huggingface.co/CompendiumLabs/bge-small-en-v1.5-gguf/resolve/main/bge-small-en-v1.5-f32.gguf\n" +
	"  #   default_dims: 384\n" +
	"  # reranking:\n" +
	"  #   style: openai-compatible\n" +
	"  #   endpoint: http://127.0.0.1:1234/v1\n" +
	"  #   name: qwen3-reranker-0.6b      \n" +
	"  #   apiKey:\n"

type initSeedResult struct {
	name   string
	path   string
	status string
}

func runInit(projectRoot string, stdout io.Writer, stderr io.Writer) int {
	absRoot, err := filepath.Abs(projectRoot)
	if err != nil {
		fmt.Fprintf(stderr, "init %s: %v\n", projectRoot, err)
		return ExitRuntimeError
	}

	configDir := filepath.Join(absRoot, ".speecdex")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		fmt.Fprintf(stderr, "init %s: %v\n", configDir, err)
		return ExitRuntimeError
	}

	results := make([]initSeedResult, 0, 2)
	for _, seed := range []struct {
		name     string
		basename string
		contents string
	}{
		{name: "config", basename: "config", contents: defaultConfigYAML},
		{name: "llms", basename: "llms", contents: defaultLLMsYAML},
	} {
		result, err := seedInitFile(configDir, seed.name, seed.basename, seed.contents)
		if err != nil {
			fmt.Fprintf(stderr, "init %s: %v\n", result.path, err)
			return ExitRuntimeError
		}
		results = append(results, result)
	}

	fmt.Fprintf(stdout, "Config directory: %s\n", configDir)
	for _, result := range results {
		fmt.Fprintf(stdout, "%s: %s %s\n", result.name, result.status, result.path)
	}
	return ExitOK
}

func seedInitFile(configDir string, name string, basename string, contents string) (initSeedResult, error) {
	yamlPath := filepath.Join(configDir, basename+".yaml")
	if initPathExists(yamlPath) {
		return initSeedResult{name: name, path: yamlPath, status: "existing"}, nil
	}

	ymlPath := filepath.Join(configDir, basename+".yml")
	if initPathExists(ymlPath) {
		return initSeedResult{name: name, path: ymlPath, status: "existing"}, nil
	}

	file, err := os.OpenFile(yamlPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return initSeedResult{name: name, path: yamlPath, status: "existing"}, nil
		}
		return initSeedResult{name: name, path: yamlPath, status: "created"}, err
	}
	if _, err := io.WriteString(file, contents); err != nil {
		_ = file.Close()
		return initSeedResult{name: name, path: yamlPath, status: "created"}, err
	}
	if err := file.Close(); err != nil {
		return initSeedResult{name: name, path: yamlPath, status: "created"}, err
	}

	return initSeedResult{name: name, path: yamlPath, status: "created"}, nil
}

func initPathExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

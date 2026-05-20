package cli

import (
	_ "embed"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const docsSearchSkillName = "docs-search"

//go:embed docs_search_skill.md
var docsSearchSkill string

type skillInstallResult struct {
	agent  string
	path   string
	status string
}

func runInstallSkill(projectRoot string, stdout io.Writer, stderr io.Writer) int {
	absRoot, err := filepath.Abs(projectRoot)
	if err != nil {
		fmt.Fprintf(stderr, "install-skill %s: %v\n", projectRoot, err)
		return ExitRuntimeError
	}

	results, err := installProjectSkills(absRoot)
	if err != nil {
		fmt.Fprintf(stderr, "install-skill: %v\n", err)
		return ExitRuntimeError
	}

	if len(results) == 0 {
		fmt.Fprintln(stdout, "No supported local agent installations found.")
		return ExitOK
	}

	for _, result := range results {
		fmt.Fprintf(stdout, "%s: %s %s\n", result.agent, result.status, result.path)
	}
	return ExitOK
}

func installProjectSkills(projectRoot string) ([]skillInstallResult, error) {
	targets := []struct {
		agent      string
		configDir  string
		markerFile string
	}{
		{agent: "codex", configDir: ".codex", markerFile: "AGENTS.md"},
		{agent: "claude", configDir: ".claude", markerFile: "CLAUDE.md"},
	}

	results := make([]skillInstallResult, 0, len(targets))
	for _, target := range targets {
		configDir := filepath.Join(projectRoot, target.configDir)
		hasConfigDir, err := localAgentDirExists(configDir)
		if err != nil {
			return nil, err
		}
		if !hasConfigDir && !localMarkerFileExists(filepath.Join(projectRoot, target.markerFile)) {
			continue
		}

		skillPath := filepath.Join(configDir, "skills", docsSearchSkillName, "SKILL.md")
		status, err := installSkillFile(skillPath, docsSearchSkill)
		if err != nil {
			return nil, err
		}
		results = append(results, skillInstallResult{
			agent:  target.agent,
			path:   skillPath,
			status: status,
		})
	}

	return results, nil
}

func installSkillFile(path string, contents string) (string, error) {
	if localFileExists(path) {
		return "existing", nil
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "created", err
	}

	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return "existing", nil
		}
		return "created", err
	}
	if _, err := io.WriteString(file, contents); err != nil {
		_ = file.Close()
		return "created", err
	}
	if err := file.Close(); err != nil {
		return "created", err
	}

	return "created", nil
}

func localAgentDirExists(path string) (bool, error) {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.IsDir() {
		return false, fmt.Errorf("%s is not a directory", path)
	}
	return true, nil
}

func localMarkerFileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func localFileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

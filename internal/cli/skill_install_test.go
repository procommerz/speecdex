package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/procommerz/speecdex-search/internal/storage"
)

func TestRunInstallSkillInstallsForDetectedLocalAgents(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		setup         func(t *testing.T, projectRoot string)
		wantCodex     bool
		wantClaude    bool
		wantStdout    []string
		wantNoStdout  []string
		wantCodexDir  bool
		wantClaudeDir bool
	}{
		{
			name: "existing codex directory installs codex skill",
			setup: func(t *testing.T, projectRoot string) {
				t.Helper()
				mkdirProjectDir(t, projectRoot, ".codex")
			},
			wantCodex:     true,
			wantStdout:    []string{"codex: created "},
			wantNoStdout:  []string{"claude:"},
			wantCodexDir:  true,
			wantClaudeDir: false,
		},
		{
			name: "existing claude directory installs claude skill",
			setup: func(t *testing.T, projectRoot string) {
				t.Helper()
				mkdirProjectDir(t, projectRoot, ".claude")
			},
			wantClaude:    true,
			wantStdout:    []string{"claude: created "},
			wantNoStdout:  []string{"codex:"},
			wantCodexDir:  false,
			wantClaudeDir: true,
		},
		{
			name: "AGENTS marker creates codex directory and installs skill",
			setup: func(t *testing.T, projectRoot string) {
				t.Helper()
				writeProjectFile(t, projectRoot, "AGENTS.md", "# Agents\n")
			},
			wantCodex:     true,
			wantStdout:    []string{"codex: created "},
			wantNoStdout:  []string{"claude:"},
			wantCodexDir:  true,
			wantClaudeDir: false,
		},
		{
			name: "CLAUDE marker creates claude directory and installs skill",
			setup: func(t *testing.T, projectRoot string) {
				t.Helper()
				writeProjectFile(t, projectRoot, "CLAUDE.md", "# Claude\n")
			},
			wantClaude:    true,
			wantStdout:    []string{"claude: created "},
			wantNoStdout:  []string{"codex:"},
			wantCodexDir:  false,
			wantClaudeDir: true,
		},
		{
			name: "both local agent setups install both skills",
			setup: func(t *testing.T, projectRoot string) {
				t.Helper()
				mkdirProjectDir(t, projectRoot, ".codex")
				writeProjectFile(t, projectRoot, "CLAUDE.md", "# Claude\n")
			},
			wantCodex:     true,
			wantClaude:    true,
			wantStdout:    []string{"codex: created ", "claude: created "},
			wantCodexDir:  true,
			wantClaudeDir: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			projectRoot := t.TempDir()
			userHome := t.TempDir()
			tt.setup(t, projectRoot)

			var stdout bytes.Buffer
			var stderr bytes.Buffer
			code := runForTest(t, []string{"--install-skill"}, &stdout, &stderr, projectRoot, userHome)
			if code != ExitOK {
				t.Fatalf("Run() exit code = %d, want %d; stderr = %q", code, ExitOK, stderr.String())
			}
			if stderr.Len() != 0 {
				t.Fatalf("Run() wrote unexpected stderr: %q", stderr.String())
			}

			if tt.wantCodex {
				assertFileContents(t, codexSkillPath(projectRoot), docsSearchSkill)
			} else {
				assertPathMissing(t, codexSkillPath(projectRoot))
			}
			if tt.wantClaude {
				assertFileContents(t, claudeSkillPath(projectRoot), docsSearchSkill)
			} else {
				assertPathMissing(t, claudeSkillPath(projectRoot))
			}

			assertAgentDirState(t, filepath.Join(projectRoot, ".codex"), tt.wantCodexDir)
			assertAgentDirState(t, filepath.Join(projectRoot, ".claude"), tt.wantClaudeDir)
			assertPathMissing(t, storage.ArtifactPath(projectRoot))

			gotStdout := stdout.String()
			for _, want := range tt.wantStdout {
				if !strings.Contains(gotStdout, want) {
					t.Fatalf("stdout = %q, want to contain %q", gotStdout, want)
				}
			}
			for _, unwanted := range tt.wantNoStdout {
				if strings.Contains(gotStdout, unwanted) {
					t.Fatalf("stdout = %q, want not to contain %q", gotStdout, unwanted)
				}
			}
		})
	}
}

func TestRunInstallSkillDoesNothingWhenNoLocalAgentIsDetected(t *testing.T) {
	t.Parallel()

	projectRoot := t.TempDir()
	userHome := t.TempDir()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := runForTest(t, []string{"--install-skill"}, &stdout, &stderr, projectRoot, userHome)
	if code != ExitOK {
		t.Fatalf("Run() exit code = %d, want %d; stderr = %q", code, ExitOK, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("Run() wrote unexpected stderr: %q", stderr.String())
	}

	if got := stdout.String(); got != "No supported local agent installations found.\n" {
		t.Fatalf("stdout = %q, want no-op summary", got)
	}
	assertPathMissing(t, filepath.Join(projectRoot, ".codex"))
	assertPathMissing(t, filepath.Join(projectRoot, ".claude"))
	assertPathMissing(t, storage.ArtifactPath(projectRoot))
}

func TestRunInstallSkillPreservesExistingSkillFiles(t *testing.T) {
	t.Parallel()

	projectRoot := t.TempDir()
	userHome := t.TempDir()
	existing := "user-owned skill\n"
	writeProjectFile(t, projectRoot, ".codex/skills/docs-search/SKILL.md", existing)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := runForTest(t, []string{"--install-skill"}, &stdout, &stderr, projectRoot, userHome)
	if code != ExitOK {
		t.Fatalf("Run() exit code = %d, want %d; stderr = %q", code, ExitOK, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("Run() wrote unexpected stderr: %q", stderr.String())
	}

	assertFileContents(t, codexSkillPath(projectRoot), existing)
	if !strings.Contains(stdout.String(), "codex: existing "+codexSkillPath(projectRoot)) {
		t.Fatalf("stdout = %q, want existing status", stdout.String())
	}
	assertPathMissing(t, storage.ArtifactPath(projectRoot))
}

func TestPackagedSkillMatchesSourceSkillTemplate(t *testing.T) {
	t.Parallel()

	assertFileContents(t, filepath.Join("..", "..", "docs", "skill", "SKILL.md"), docsSearchSkill)
}

func mkdirProjectDir(t *testing.T, root string, name string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(name)), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
}

func codexSkillPath(projectRoot string) string {
	return filepath.Join(projectRoot, ".codex", "skills", docsSearchSkillName, "SKILL.md")
}

func claudeSkillPath(projectRoot string) string {
	return filepath.Join(projectRoot, ".claude", "skills", docsSearchSkillName, "SKILL.md")
}

func assertAgentDirState(t *testing.T, path string, wantExists bool) {
	t.Helper()

	info, err := os.Stat(path)
	if !wantExists {
		if !os.IsNotExist(err) {
			t.Fatalf("Stat(%s) error = %v, want missing path", path, err)
		}
		return
	}
	if err != nil {
		t.Fatalf("Stat(%s) error = %v", path, err)
	}
	if !info.IsDir() {
		t.Fatalf("%s is not a directory", path)
	}
}

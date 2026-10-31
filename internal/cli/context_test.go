package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestContextSync(t *testing.T) {
	home := t.TempDir()
	t.Setenv("FORGE_HOME", home)
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "go.mod"), []byte("module x\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Unregistered directory, no CLAUDE.md: default target is AGENTS.md only.
	out, err := run(t, "context", "sync", project, "--dry-run")
	if err != nil || !strings.Contains(out, "AGENTS.md              would be created") || strings.Contains(out, "CLAUDE.md") {
		t.Fatalf("dry run: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(project, "AGENTS.md")); err == nil {
		t.Fatal("--dry-run wrote a file")
	}

	out, err = run(t, "context", "sync", project, "--target", "agents,claude")
	if err != nil || !strings.Contains(out, "AGENTS.md              created") || !strings.Contains(out, "CLAUDE.md              created") {
		t.Fatalf("sync: %v\n%s", err, out)
	}
	if out, err = run(t, "context", "sync", project, "--target", "agents,claude"); err != nil || strings.Count(out, "unchanged") != 2 {
		t.Fatalf("resync: %v\n%s", err, out)
	}

	if _, err := run(t, "context", "sync", project, "--target", "kimi"); err == nil {
		t.Fatal("unknown target should fail")
	}

	// Config file chooses targets when --target is absent.
	cfg := "context:\n  targets: [claude]\n"
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err = run(t, "context", "sync", project); err != nil || !strings.Contains(out, "CLAUDE.md              updated") || strings.Contains(out, "AGENTS.md") {
		t.Fatalf("config targets: %v\n%s", err, out)
	}
}

package core

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestUpsertSection(t *testing.T) {
	block := func(body string) string { return BeginMarker + "\n" + body + "\n" + EndMarker + "\n" }

	t.Run("empty file gets a title", func(t *testing.T) {
		got, err := UpsertSection("", "app", "facts")
		if err != nil || got != "# app\n\n"+block("facts") {
			t.Fatalf("%q %v", got, err)
		}
	})
	t.Run("hand-written file: block appended, text kept", func(t *testing.T) {
		mine := "# My notes\n\nNever deploy on Fridays.\n"
		got, err := UpsertSection(mine, "app", "facts")
		if err != nil || got != mine+"\n"+block("facts") {
			t.Fatalf("%q %v", got, err)
		}
	})
	t.Run("existing block replaced, text before and after kept byte for byte", func(t *testing.T) {
		before, after := "# Mine\n\nintro\n\n", "\n## Also mine\n\n  indented  \n"
		got, err := UpsertSection(before+block("old facts")+after, "app", "new facts")
		if err != nil || got != before+block("new facts")+after {
			t.Fatalf("%q %v", got, err)
		}
	})
	t.Run("idempotent", func(t *testing.T) {
		once, _ := UpsertSection("# Mine\n", "app", "facts")
		twice, err := UpsertSection(once, "app", "facts")
		if err != nil || once != twice {
			t.Fatalf("second run changed the file:\n%q\n%q", once, twice)
		}
	})
	t.Run("markers mentioned in text are not markers", func(t *testing.T) {
		mine := "Sync owns the `<!-- forge:begin/end -->` block.\n" +
			"Inline: `" + BeginMarker + "` and `" + EndMarker + "`.\n" +
			"<!-- forge:begin/end -->\n" +
			"<!-- forge:beginning -->\n" +
			"  " + EndMarker + "\n"
		once, err := UpsertSection(mine, "app", "facts")
		if err != nil || once != mine+"\n"+block("facts") {
			t.Fatalf("%q %v", once, err)
		}
		twice, err := UpsertSection(once, "app", "new facts")
		if err != nil || twice != mine+"\n"+block("new facts") {
			t.Fatalf("%q %v", twice, err)
		}
	})
	t.Run("marker lines tolerate CRLF and trailing spaces", func(t *testing.T) {
		in := "mine\r\n" + BeginMarker + "\r\nold\r\n" + EndMarker + "  \r\nmore\r\n"
		got, err := UpsertSection(in, "app", "facts")
		if err != nil || got != "mine\r\n"+block("facts")+"more\r\n" {
			t.Fatalf("%q %v", got, err)
		}
	})
	t.Run("end marker at end of file without newline", func(t *testing.T) {
		got, err := UpsertSection("mine\n"+BeginMarker+"\nold\n"+EndMarker, "app", "facts")
		if err != nil || got != "mine\n"+block("facts") {
			t.Fatalf("%q %v", got, err)
		}
	})
	t.Run("a shorter begin notice is still the begin marker", func(t *testing.T) {
		got, err := UpsertSection("<!-- forge:begin -->\nold\n"+EndMarker+"\n", "app", "facts")
		if err != nil || got != block("facts") {
			t.Fatalf("%q %v", got, err)
		}
	})
	for name, in := range map[string]string{
		"begin without end": "x\n" + BeginMarker + "\nfacts\n",
		"end without begin": "x\n" + EndMarker + "\n",
		"two blocks":        block("a") + block("b"),
		"end before begin":  EndMarker + "\n" + BeginMarker + "\n",
	} {
		t.Run("malformed: "+name, func(t *testing.T) {
			if _, err := UpsertSection(in, "app", "facts"); !errors.Is(err, ErrMalformedMarkers) {
				t.Fatalf("want ErrMalformedMarkers, got %v", err)
			}
		})
	}
}

func TestGatherAndRender(t *testing.T) {
	root := t.TempDir()
	write(t, root, map[string]string{
		"api/package.json":   `{"dependencies":{"@nestjs/core":"1"},"scripts":{"build":"x","test":"x","start":"x","weird":"x"}}`,
		"api/pnpm-lock.yaml": "",
		"api/src/main.ts":    "",
		"node_modules/x/y":   "",
		"go.mod":             "",
		"justfile":           "",
		"compose.yaml":       "",
		".env":               "DATABASE_URL=postgres://u:supersecret@db/app\nJWT_SECRET=hunter2\n",
	})

	f, err := GatherFacts(context.Background(), root, "app", []string{"go", "nestjs"}, "compose.yaml", ".env")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(f.EnvKeys, []string{"DATABASE_URL", "JWT_SECRET"}) {
		t.Errorf("env keys: %v", f.EnvKeys)
	}
	if !slices.Equal(f.Layout, []string{"api/", "compose.yaml", "go.mod", "justfile"}) {
		t.Errorf("layout (not a git repo: hidden + node_modules skipped): %v", f.Layout)
	}

	md := RenderFacts(f)
	for _, want := range []string{
		"**Stack:** go, nestjs",
		"`DATABASE_URL`, `JWT_SECRET`",
		"| build (api) | `cd api && pnpm run build` |",
		"| test (api) | `cd api && pnpm test` |",
		"| start (api) | `cd api && pnpm start` |",
		"| test | `go test ./...` |",
		"| task runner | `just --list` |",
		"`docker compose -f compose.yaml up -d`",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("render missing %q\n%s", want, md)
		}
	}
	for _, secret := range []string{"supersecret", "hunter2", "weird"} {
		if strings.Contains(md, secret) {
			t.Errorf("render must not contain %q", secret)
		}
	}
}

func TestSyncContext(t *testing.T) {
	root := t.TempDir()
	mine := "# Hand-written\n\nUse tabs.\n"
	if err := os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte(mine), 0o600); err != nil {
		t.Fatal(err)
	}
	f := Facts{Name: "app", Stack: []string{"go"}}

	if got := DefaultTargets(root); !slices.Equal(got, []string{"agents", "claude"}) {
		t.Fatalf("default targets with CLAUDE.md present: %v", got)
	}

	// Dry run touches nothing.
	if _, err := SyncContext(root, f, []string{"agents", "claude"}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "AGENTS.md")); err == nil {
		t.Fatal("dry run created AGENTS.md")
	}

	res, err := SyncContext(root, f, []string{"agents", "claude"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if !res[0].Created || !res[1].Changed || res[1].Created {
		t.Fatalf("results: %+v", res)
	}
	agents, _ := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	claude, _ := os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	if !strings.Contains(string(agents), "**Stack:** go") {
		t.Errorf("AGENTS.md has no facts:\n%s", agents)
	}
	if !strings.HasPrefix(string(claude), mine) || !strings.Contains(string(claude), "\n@AGENTS.md\n") ||
		strings.Contains(string(claude), "**Stack:**") {
		t.Errorf("CLAUDE.md should keep hand-written text and only import AGENTS.md:\n%s", claude)
	}
	if info, _ := os.Stat(filepath.Join(root, "CLAUDE.md")); info.Mode().Perm() != 0o600 {
		t.Errorf("permissions not preserved: %v", info.Mode().Perm())
	}

	// Second run: nothing changes.
	res, err = SyncContext(root, f, []string{"agents", "claude"}, true)
	if err != nil || res[0].Changed || res[1].Changed {
		t.Fatalf("second run should be a no-op: %v %+v", err, res)
	}

	// Claude alone gets the full facts, not an import of a file it won't read.
	res, err = SyncContext(root, f, []string{"claude"}, true)
	if err != nil || !strings.Contains(res[0].Content, "**Stack:** go") || strings.Contains(res[0].Content, "@AGENTS.md") {
		t.Fatalf("claude-only: %v\n%s", err, res[0].Content)
	}
}

func TestSyncContextWritesNothingWhenAnyTargetFails(t *testing.T) {
	for name, breaks := range map[string]string{
		"later target is bad":   "CLAUDE.md",
		"earlier target is bad": "AGENTS.md",
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			bad := "x\n" + BeginMarker + "\nno end marker\n"
			good := "# Mine\n"
			files := map[string]string{"AGENTS.md": good, "CLAUDE.md": good}
			files[breaks] = bad
			for file, content := range files {
				if err := os.WriteFile(filepath.Join(root, file), []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
			}

			_, err := SyncContext(root, Facts{Name: "app"}, []string{"agents", "claude"}, true)
			if !errors.Is(err, ErrMalformedMarkers) || !strings.Contains(err.Error(), breaks) {
				t.Fatalf("want ErrMalformedMarkers naming %s, got %v", breaks, err)
			}
			for file, content := range files {
				got, _ := os.ReadFile(filepath.Join(root, file))
				if string(got) != content {
					t.Errorf("%s was changed by a failed sync:\n%s", file, got)
				}
			}
		})
	}

	t.Run("missing file is not created when another target is bad", func(t *testing.T) {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte(BeginMarker+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := SyncContext(root, Facts{Name: "app"}, []string{"agents", "claude"}, true); err == nil {
			t.Fatal("want an error")
		}
		if _, err := os.Stat(filepath.Join(root, "AGENTS.md")); err == nil {
			t.Fatal("AGENTS.md was created by a failed sync")
		}
	})
}

func TestNormalizeTargets(t *testing.T) {
	if got, err := NormalizeTargets([]string{" Claude", "agents", "claude", ""}); err != nil || !slices.Equal(got, []string{"claude", "agents"}) {
		t.Fatalf("%v %v", got, err)
	}
	if _, err := NormalizeTargets([]string{"deepseek"}); err == nil || !strings.Contains(err.Error(), "valid: agents, claude") {
		t.Fatalf("unknown target: %v", err)
	}
	if _, err := NormalizeTargets(nil); err == nil {
		t.Fatal("empty targets should fail")
	}
}

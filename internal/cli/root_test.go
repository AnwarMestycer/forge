package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// run executes forge with args and returns combined stdout/stderr.
func run(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	root := NewRootCmd("1.2.3")
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

func TestVersion(t *testing.T) {
	out, err := run(t, "--version")
	if err != nil {
		t.Fatal(err)
	}
	if out != "forge 1.2.3\n" {
		t.Fatalf("got %q", out)
	}
}

func TestStubCommandsExist(t *testing.T) {
	for _, name := range []string{"context", "vouch", "chaos"} {
		out, err := run(t, name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !strings.Contains(out, "not implemented yet") {
			t.Fatalf("%s: got %q", name, out)
		}
	}
}

func TestAddAndList(t *testing.T) {
	t.Setenv("FORGE_HOME", t.TempDir())
	project := t.TempDir()

	out, err := run(t, "list")
	if err != nil || !strings.Contains(out, "no projects yet") {
		t.Fatalf("empty list: %v %q", err, out)
	}

	if out, err = run(t, "add", project, "--name", "demo"); err != nil {
		t.Fatalf("add: %v %q", err, out)
	}
	// Re-adding refreshes in place: no duplicate, name kept without --name.
	if out, err = run(t, "add", project); err != nil || !strings.HasPrefix(out, "updated demo ") {
		t.Fatalf("re-add: %v %q", err, out)
	}
	if out, err = run(t, "add", project, "--name", "renamed"); err != nil || !strings.HasPrefix(out, "updated renamed ") {
		t.Fatalf("re-add with --name: %v %q", err, out)
	}

	out, err = run(t, "list")
	if err != nil || !strings.Contains(out, "renamed") || strings.Count(out, project) != 1 {
		t.Fatalf("list: %v %q", err, out)
	}
}

func TestAddDetectsAndOverrides(t *testing.T) {
	t.Setenv("FORGE_HOME", t.TempDir())
	project := t.TempDir()
	for name, body := range map[string]string{
		"package.json":        `{"dependencies":{"@nestjs/core":"11"}}`,
		"docker-compose.yml":  "",
		".env":                "SECRET=hunter2",
		"deploy/compose.prod": "",
	} {
		p := filepath.Join(project, name)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	out, err := run(t, "add", project)
	if err != nil {
		t.Fatalf("add: %v %q", err, out)
	}
	for _, want := range []string{"stack:   nestjs", "compose: docker-compose.yml", "env:     .env"} {
		if !strings.Contains(out, want) {
			t.Fatalf("add output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "hunter2") {
		t.Fatal("env contents must never be printed")
	}

	other := t.TempDir()
	if err := os.WriteFile(filepath.Join(other, "c.yml"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	out, err = run(t, "add", other, "--stack", "Flutter, supabase,flutter", "--compose", "c.yml")
	if err != nil || !strings.Contains(out, "stack:   flutter, supabase") || !strings.Contains(out, "compose: c.yml") {
		t.Fatalf("overrides: %v\n%s", err, out)
	}

	if _, err := run(t, "add", t.TempDir(), "--env", "nope.env"); err == nil {
		t.Fatal("--env pointing at a missing file should fail")
	}

	out, err = run(t, "list")
	if err != nil || !strings.Contains(out, "STACK") || !strings.Contains(out, "nestjs") || !strings.Contains(out, "flutter,supabase") {
		t.Fatalf("list: %v\n%s", err, out)
	}
}

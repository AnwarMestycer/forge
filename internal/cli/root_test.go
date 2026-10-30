package cli

import (
	"bytes"
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
	for _, name := range []string{"switch", "context", "vouch", "chaos"} {
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
	if _, err = run(t, "add", project); err == nil {
		t.Fatal("adding the same path twice should fail")
	}

	out, err = run(t, "list")
	if err != nil || !strings.Contains(out, "demo") || !strings.Contains(out, project) {
		t.Fatalf("list: %v %q", err, out)
	}
}

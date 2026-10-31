package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeCompose installs a script as FORGE_COMPOSE that records its args, its
// working directory and $DB_URL (from the project's env file) to a log file.
func fakeCompose(t *testing.T) (logPath string) {
	t.Helper()
	dir := t.TempDir()
	logPath = filepath.Join(dir, "compose.log")
	script := filepath.Join(dir, "fake-compose")
	body := "#!/bin/sh\necho \"args=$*\" >> " + logPath + "\necho \"pwd=$(pwd)\" >> " + logPath + "\necho \"DB_URL=$DB_URL\" >> " + logPath + "\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FORGE_COMPOSE", script+" compose")
	return logPath
}

func gitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestSwitch(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	t.Setenv("FORGE_HOME", t.TempDir())
	log := fakeCompose(t)

	project := t.TempDir()
	project, _ = filepath.EvalSymlinks(project)
	for name, body := range map[string]string{
		"compose.yaml": "services: {}\n",
		".env":         "DB_URL=postgres://localhost/app\nSECRET=hunter2\n",
		"main.go":      "package main\n",
	} {
		if err := os.WriteFile(filepath.Join(project, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	gitCmd(t, project, "init", "-q", "-b", "feature/login")
	gitCmd(t, project, "add", "main.go")
	gitCmd(t, project, "commit", "-q", "-m", "add main")
	if err := os.WriteFile(filepath.Join(project, "wip.go"), nil, 0o600); err != nil {
		t.Fatal(err)
	}

	if out, err := run(t, "add", project, "--name", "app"); err != nil {
		t.Fatalf("add: %v %s", err, out)
	}

	out, err := run(t, "switch", "app", "--no-shell")
	if err != nil {
		t.Fatalf("switch: %v\n%s", err, out)
	}
	for _, want := range []string{
		"→ app", "starting containers (compose.yaml)", "env: 2 vars from .env",
		"last here: first switch", "branch:    feature/login", "3 uncommitted", "?? wip.go", "add main",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "hunter2") {
		t.Error("env values must never be printed")
	}

	b, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("compose was not run: %v", err)
	}
	logged := string(b)
	for _, want := range []string{
		"args=compose -f " + filepath.Join(project, "compose.yaml") + " up -d",
		"pwd=" + project,
		"DB_URL=postgres://localhost/app", // env file reaches compose for ${VAR} interpolation
	} {
		if !strings.Contains(logged, want) {
			t.Errorf("compose log missing %q:\n%s", want, logged)
		}
	}

	// Second switch: remembers the first one; --no-up skips compose; path lookup works.
	_ = os.Remove(log)
	out, err = run(t, "switch", project, "--no-shell", "--no-up")
	if err != nil || !strings.Contains(out, "last here: just now") || strings.Contains(out, "starting containers") {
		t.Fatalf("second switch: %v\n%s", err, out)
	}
	if _, err := os.Stat(log); err == nil {
		t.Error("--no-up still ran compose")
	}
}

func TestSwitchErrors(t *testing.T) {
	t.Setenv("FORGE_HOME", t.TempDir())
	fakeCompose(t)

	if _, err := run(t, "switch", "ghost", "--no-shell"); err == nil || !strings.Contains(err.Error(), `no project named "ghost"`) {
		t.Fatalf("unknown name: %v", err)
	}
	if _, err := run(t, "switch", t.TempDir(), "--no-shell"); err == nil || !strings.Contains(err.Error(), "not registered") {
		t.Fatalf("unregistered path: %v", err)
	}

	a, b := t.TempDir(), t.TempDir()
	_, _ = run(t, "add", a, "--name", "api")
	_, _ = run(t, "add", b, "--name", "api")
	if _, err := run(t, "switch", "api", "--no-shell"); err == nil || !strings.Contains(err.Error(), "2 projects are named") {
		t.Fatalf("ambiguous name: %v", err)
	}

	gone := t.TempDir()
	_, _ = run(t, "add", gone, "--name", "gone")
	_ = os.RemoveAll(gone)
	if _, err := run(t, "switch", "gone", "--no-shell"); err == nil || !strings.Contains(err.Error(), "is gone") {
		t.Fatalf("missing dir: %v", err)
	}
}

func TestAgo(t *testing.T) {
	for d, want := range map[time.Duration]string{
		10 * time.Second: "just now",
		time.Minute:      "1 minute ago",
		45 * time.Minute: "45 minutes ago",
		3 * time.Hour:    "3 hours ago",
		50 * time.Hour:   "2 days ago",
	} {
		if got := ago(d); got != want {
			t.Errorf("ago(%v) = %q, want %q", d, got, want)
		}
	}
}

package core

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/subosito/gotenv"
)

// ResolvePath turns a stored project-relative path (compose_file, env_file)
// into an absolute one. Empty stays empty.
func ResolvePath(root, p string) string {
	if p == "" || filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(root, p)
}

// LoadEnv parses a dotenv file into KEY=VALUE pairs, sorted by key.
func LoadEnv(path string) ([]string, error) {
	env, err := gotenv.Read(path)
	if err != nil {
		return nil, fmt.Errorf("read env file %s: %w", path, err)
	}
	out := make([]string, 0, len(env))
	for k, v := range env {
		out = append(out, k+"="+v)
	}
	sort.Strings(out)
	return out, nil
}

// ComposeCommand is the compose invocation: $FORGE_COMPOSE (e.g.
// "podman compose") or "docker compose".
func ComposeCommand() []string {
	if c := strings.Fields(os.Getenv("FORGE_COMPOSE")); len(c) > 0 {
		return c
	}
	return []string{"docker", "compose"}
}

// ComposeUp runs `<compose> -f composeFile up -d` in dir. env is added to
// forge's own environment so compose can interpolate ${VARS} from the
// project's env file. Output streams to stdout/stderr.
func ComposeUp(ctx context.Context, dir, composeFile string, env []string, stdout, stderr io.Writer) error {
	c := ComposeCommand()
	args := slices.Concat(c[1:], []string{"-f", composeFile, "up", "-d"})
	cmd := exec.CommandContext(ctx, c[0], args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if err := cmd.Run(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return fmt.Errorf("%s not found — install Docker, set FORGE_COMPOSE (e.g. \"podman compose\"), or use --no-up", c[0])
		}
		return fmt.Errorf("%s up failed (%w) — is the container engine running? (`sudo systemctl start docker`) Or skip containers with --no-up", strings.Join(c, " "), err)
	}
	return nil
}

// GitState is a snapshot of a repo for "where you left off".
type GitState struct {
	Branch  string
	Changed []string // `git status --porcelain` lines (modified, staged, untracked)
	Recent  []string // last commits: "<hash> <subject> (<relative date>)"
}

// ReadGitState inspects the repo containing dir. ok is false when dir is not
// inside a git work tree (or git is not installed).
func ReadGitState(ctx context.Context, dir string) (st GitState, ok bool, err error) {
	git := func(args ...string) (string, error) {
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = dir
		var out, errb bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &errb
		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(errb.String()))
		}
		return strings.TrimRight(out.String(), "\n"), nil
	}

	if out, err := git("rev-parse", "--is-inside-work-tree"); err != nil || out != "true" {
		return st, false, nil
	}
	if st.Branch, err = git("rev-parse", "--abbrev-ref", "HEAD"); err != nil {
		// A fresh repo with no commits has no HEAD yet.
		st.Branch, _ = git("symbolic-ref", "--short", "HEAD")
	}
	status, err := git("status", "--porcelain", "--", ".")
	if err != nil {
		return st, true, err
	}
	st.Changed = lines(status)
	// No commits yet is not an error worth reporting.
	if log, err := git("log", "-3", "--format=%h %s (%cr)", "--", "."); err == nil {
		st.Recent = lines(log)
	}
	return st, true, nil
}

func lines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// Shell returns the user's login shell ($SHELL), falling back to /bin/sh.
func Shell() string {
	if s := os.Getenv("SHELL"); s != "" {
		return s
	}
	return "/bin/sh"
}

// RunShell starts an interactive shell in dir with env added and
// FORGE_PROJECT=name set, and waits for the user to exit it. The shell's own
// exit status (e.g. from the last command typed) is not treated as an error.
func RunShell(dir, name string, env []string) error {
	cmd := exec.Command(Shell())
	cmd.Dir = dir
	cmd.Env = append(append(os.Environ(), env...), "FORGE_PROJECT="+name)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	err := cmd.Run()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return nil
	}
	return err
}

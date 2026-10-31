package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/subosito/gotenv"
)

// Facts is forge's tool-neutral picture of a project. Every context target
// (AGENTS.md, CLAUDE.md, …) is rendered from the same Facts, so they can't
// drift apart. Nothing time-dependent goes in here: re-running sync on an
// unchanged project must produce an identical file.
type Facts struct {
	Name        string
	Stack       []string
	ComposeFile string
	EnvFile     string
	EnvKeys     []string  // variable names only — values never leave the env file
	Layout      []string  // top-level entries, dirs suffixed with "/"
	Commands    []Command // how to run / build / test
	Recent      []string  // recent commit subjects touching the project
}

// Command is one "how do I…" entry, e.g. {"test (api)", "cd api && npm test"}.
type Command struct {
	What, Run string
}

const recentCommits = 8

// GatherFacts inspects the project at root. name and stack/compose/env come
// from the registered project (or detection for unregistered directories).
func GatherFacts(ctx context.Context, root, name string, stack []string, composeFile, envFile string) (Facts, error) {
	f := Facts{Name: name, Stack: stack, ComposeFile: composeFile, EnvFile: envFile}

	if envFile != "" {
		if env, err := gotenv.Read(ResolvePath(root, envFile)); err == nil {
			for k := range env {
				f.EnvKeys = append(f.EnvKeys, k)
			}
			slices.Sort(f.EnvKeys)
		}
	}

	layout, err := topLevel(ctx, root)
	if err != nil {
		return f, err
	}
	f.Layout = layout

	f.Commands = commands(root, layout, composeFile)

	// Not a git repo (or no commits yet) just means no history section.
	if log, err := gitOut(ctx, root, "log", fmt.Sprintf("-%d", recentCommits), "--format=%s", "--", "."); err == nil {
		f.Recent = lines(log)
	}
	return f, nil
}

// topLevel lists the project's top-level files and directories. In a git repo
// it uses tracked files (so .gitignore'd build output and node_modules never
// show up); otherwise it reads the directory, skipping hidden and build dirs.
func topLevel(ctx context.Context, root string) ([]string, error) {
	seen := map[string]bool{}
	if out, err := gitOut(ctx, root, "ls-files", "--", "."); err == nil && out != "" {
		for _, l := range lines(out) {
			first, rest, isDir := strings.Cut(l, "/")
			if isDir && rest != "" {
				first += "/"
			}
			seen[first] = true
		}
	} else {
		entries, err := os.ReadDir(root)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			n := e.Name()
			if strings.HasPrefix(n, ".") || skipDirs[n] {
				continue
			}
			if e.IsDir() {
				n += "/"
			}
			seen[n] = true
		}
	}
	out := make([]string, 0, len(seen))
	for n := range seen {
		// The context files are forge's own output: listing them would make
		// the first sync change the second one's result.
		if !isTargetFile(n) && n != ".gitkeep" {
			out = append(out, n)
		}
	}
	// Directories first, then files, each alphabetical.
	slices.SortFunc(out, func(a, b string) int {
		ad, bd := strings.HasSuffix(a, "/"), strings.HasSuffix(b, "/")
		if ad != bd {
			if ad {
				return -1
			}
			return 1
		}
		return strings.Compare(a, b)
	})
	return out, nil
}

// commands derives run/build/test commands from what's in the project root
// and its top-level directories.
func commands(root string, layout []string, composeFile string) []Command {
	var cmds []Command
	dirs := []string{"."}
	for _, l := range layout {
		if d, ok := strings.CutSuffix(l, "/"); ok && !skipDirs[d] {
			dirs = append(dirs, d)
		}
	}
	for _, d := range dirs {
		dir := filepath.Join(root, d)
		prefix, label := "", ""
		if d != "." {
			prefix, label = "cd "+d+" && ", " ("+d+")"
		}
		switch {
		case isFile(filepath.Join(dir, "package.json")):
			pm := nodePM(dir)
			for _, s := range nodeScripts(filepath.Join(dir, "package.json")) {
				run := pm + " run " + s
				if s == "test" || s == "start" {
					run = pm + " " + s
				}
				cmds = append(cmds, Command{s + label, prefix + run})
			}
		case isFile(filepath.Join(dir, "pubspec.yaml")):
			cmds = append(cmds, Command{"run" + label, prefix + "flutter run"}, Command{"test" + label, prefix + "flutter test"})
		}
		if isFile(filepath.Join(dir, "go.mod")) {
			cmds = append(cmds, Command{"build" + label, prefix + "go build ./..."}, Command{"test" + label, prefix + "go test ./..."})
		}
		if isFile(filepath.Join(dir, "Cargo.toml")) {
			cmds = append(cmds, Command{"build" + label, prefix + "cargo build"}, Command{"test" + label, prefix + "cargo test"})
		}
	}
	if isFile(filepath.Join(root, "justfile")) || isFile(filepath.Join(root, "Justfile")) {
		cmds = append(cmds, Command{"task runner", "just --list"})
	}
	if composeFile != "" {
		cmds = append(cmds, Command{"start services", "docker compose -f " + composeFile + " up -d"})
	}
	return cmds
}

// nodeScriptOrder lists the package.json scripts worth telling an agent about.
var nodeScriptOrder = []string{"dev", "start", "build", "test", "test:e2e", "lint", "typecheck", "format"}

func nodeScripts(path string) []string {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var pkg struct {
		Scripts map[string]string `json:"scripts"`
	}
	if json.Unmarshal(b, &pkg) != nil {
		return nil
	}
	var out []string
	for _, s := range nodeScriptOrder {
		if _, ok := pkg.Scripts[s]; ok {
			out = append(out, s)
		}
	}
	return out
}

// nodePM picks the package manager from the lockfile.
func nodePM(dir string) string {
	switch {
	case isFile(filepath.Join(dir, "pnpm-lock.yaml")):
		return "pnpm"
	case isFile(filepath.Join(dir, "yarn.lock")):
		return "yarn"
	case isFile(filepath.Join(dir, "bun.lockb")), isFile(filepath.Join(dir, "bun.lock")):
		return "bun"
	default:
		return "npm"
	}
}

func gitOut(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	return strings.TrimRight(string(out), "\n"), err
}

// Markers delimit the part of a context file that forge owns. Everything
// outside them belongs to the user and is never touched.
const (
	BeginMarker = "<!-- forge:begin — generated by `forge context sync`; edits inside this block are overwritten -->"
	EndMarker   = "<!-- forge:end -->"
	beginPrefix = "<!-- forge:begin"
)

// ErrMalformedMarkers means a file has a begin marker without an end (or
// vice versa, or several) — forge refuses to guess which text it owns.
var ErrMalformedMarkers = errors.New("forge markers are malformed")

// UpsertSection returns existing with the forge-managed block replaced by
// body. If there is no block yet, it is appended (or, for an empty file,
// written under title). Text outside the markers is preserved byte for byte.
func UpsertSection(existing, title, body string) (string, error) {
	block := BeginMarker + "\n" + strings.TrimSpace(body) + "\n" + EndMarker + "\n"

	begins := strings.Count(existing, beginPrefix)
	ends := strings.Count(existing, EndMarker)
	switch {
	case begins == 0 && ends == 0:
		if strings.TrimSpace(existing) == "" {
			return "# " + title + "\n\n" + block, nil
		}
		return strings.TrimRight(existing, "\n") + "\n\n" + block, nil
	case begins != 1 || ends != 1:
		return "", fmt.Errorf("%w: found %d begin and %d end markers", ErrMalformedMarkers, begins, ends)
	}

	start := strings.Index(existing, beginPrefix)
	end := strings.Index(existing, EndMarker)
	if end < start {
		return "", fmt.Errorf("%w: end marker comes before begin marker", ErrMalformedMarkers)
	}
	after := existing[end+len(EndMarker):]
	after = strings.TrimPrefix(after, "\n")
	return existing[:start] + block + after, nil
}

// RenderFacts renders Facts as the markdown body of the managed block.
func RenderFacts(f Facts) string {
	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format, a...) }

	w("## Project facts\n\n")
	w("- **Name:** %s\n", f.Name)
	if len(f.Stack) > 0 {
		w("- **Stack:** %s\n", strings.Join(f.Stack, ", "))
	}
	if f.ComposeFile != "" {
		w("- **Services:** Docker Compose (`%s`)\n", f.ComposeFile)
	}
	if f.EnvFile != "" {
		w("- **Env file:** `%s`", f.EnvFile)
		if len(f.EnvKeys) > 0 {
			keys := make([]string, len(f.EnvKeys))
			for i, k := range f.EnvKeys {
				keys[i] = "`" + k + "`"
			}
			w(" — defines %s (values are secret: never print or commit them)", strings.Join(keys, ", "))
		}
		w("\n")
	}

	if len(f.Commands) > 0 {
		w("\n## Commands\n\n| What | Run |\n|---|---|\n")
		for _, c := range f.Commands {
			w("| %s | `%s` |\n", c.What, c.Run)
		}
	}

	if len(f.Layout) > 0 {
		w("\n## Layout\n\n```\n")
		for _, l := range f.Layout {
			w("%s\n", l)
		}
		w("```\n")
	}

	if len(f.Recent) > 0 {
		w("\n## Recent work\n\n")
		for _, c := range f.Recent {
			w("- %s\n", c)
		}
	}
	return b.String()
}

// ClaudeImportBody is CLAUDE.md's managed block when AGENTS.md is also a
// target. Claude Code reads only CLAUDE.md when one exists, so this import
// is what makes it see the shared facts. The import line must stay outside
// backticks/code blocks or Claude Code ignores it.
const ClaudeImportBody = `@AGENTS.md

Project facts are kept in AGENTS.md (shared with other AI coding tools) and imported above. Put Claude-specific notes outside this block.`

// Context targets: which tool's instruction file to write.
const (
	TargetAgents = "agents" // AGENTS.md — Codex, Cursor, Copilot agent, Zed, …
	TargetClaude = "claude" // CLAUDE.md — Claude Code
)

var targetFiles = map[string]string{TargetAgents: "AGENTS.md", TargetClaude: "CLAUDE.md"}

// ValidTargets lists the supported targets, sorted.
func ValidTargets() []string {
	out := make([]string, 0, len(targetFiles))
	for t := range targetFiles {
		out = append(out, t)
	}
	slices.Sort(out)
	return out
}

// DefaultTargets is used when neither --target nor config chooses: AGENTS.md
// always (the shared standard), plus CLAUDE.md when the project already has one.
func DefaultTargets(root string) []string {
	t := []string{TargetAgents}
	if isFile(filepath.Join(root, targetFiles[TargetClaude])) {
		t = append(t, TargetClaude)
	}
	return t
}

// NormalizeTargets lowercases, dedupes and validates target names.
func NormalizeTargets(in []string) ([]string, error) {
	var out []string
	for _, t := range in {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" || slices.Contains(out, t) {
			continue
		}
		if _, ok := targetFiles[t]; !ok {
			return nil, fmt.Errorf("unknown context target %q (valid: %s)", t, strings.Join(ValidTargets(), ", "))
		}
		out = append(out, t)
	}
	if len(out) == 0 {
		return nil, errors.New("no context targets given")
	}
	return out, nil
}

// SyncResult reports what happened to one target file.
type SyncResult struct {
	Target, File string // File is relative to the project root
	Content      string // full new file content
	Changed      bool   // false: file already up to date
	Created      bool
}

// SyncContext renders f into each target's file under root. With write=false
// nothing touches disk (dry run); results still say what would change.
func SyncContext(root string, f Facts, targets []string, write bool) ([]SyncResult, error) {
	var results []SyncResult
	for _, t := range targets {
		file := targetFiles[t]
		body := RenderFacts(f)
		if t == TargetClaude && slices.Contains(targets, TargetAgents) {
			body = ClaudeImportBody
		}

		path := filepath.Join(root, file)
		old, err := os.ReadFile(path)
		created := errors.Is(err, os.ErrNotExist)
		if err != nil && !created {
			return results, err
		}
		next, err := UpsertSection(string(old), f.Name, body)
		if err != nil {
			return results, fmt.Errorf("%s: %w — fix or remove the forge markers by hand", file, err)
		}
		r := SyncResult{Target: t, File: file, Content: next, Changed: next != string(old), Created: created}
		if write && r.Changed {
			if err := writeAtomic(path, []byte(next)); err != nil {
				return results, err
			}
		}
		results = append(results, r)
	}
	return results, nil
}

// writeAtomic writes data to path via a temp file + rename, so a crash never
// leaves a half-written file. An existing file's permissions are kept.
func writeAtomic(path string, data []byte) error {
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".forge-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }() // no-op after a successful rename
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func isTargetFile(name string) bool {
	for _, f := range targetFiles {
		if name == f {
			return true
		}
	}
	return false
}

// Package core holds forge's project logic: detecting what a project is
// and (later) switching between projects and syncing their context.
package core

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Detected is what forge can infer about a project directory on its own.
// Paths are relative to the project root; empty means "not found".
type Detected struct {
	Stack       []string // e.g. ["nestjs", "supabase"], sorted, no duplicates
	ComposeFile string
	EnvFile     string
}

// composeNames is Docker Compose's own lookup order.
var composeNames = []string{"compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml"}

// skipDirs are never scanned for stack markers.
var skipDirs = map[string]bool{"node_modules": true, "vendor": true, "dist": true, "build": true, "target": true}

// Detect inspects dir (the project root and its immediate subdirectories,
// so monorepos like api/ + web/ are covered) and reports stack, compose file
// and env file.
func Detect(dir string) (Detected, error) {
	var d Detected

	stacks := map[string]bool{}
	dirs := []string{dir}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return d, err
	}
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") && !skipDirs[e.Name()] {
			dirs = append(dirs, filepath.Join(dir, e.Name()))
		}
	}
	for _, sub := range dirs {
		for _, s := range stacksIn(sub) {
			stacks[s] = true
		}
	}
	for s := range stacks {
		d.Stack = append(d.Stack, s)
	}
	slices.Sort(d.Stack)

	for _, name := range composeNames {
		if isFile(filepath.Join(dir, name)) {
			d.ComposeFile = name
			break
		}
	}
	if isFile(filepath.Join(dir, ".env")) {
		d.EnvFile = ".env"
	}
	return d, nil
}

// stacksIn returns the stacks whose marker files are directly in dir.
func stacksIn(dir string) []string {
	var out []string
	if s := nodeStack(filepath.Join(dir, "package.json")); s != "" {
		out = append(out, s)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "pubspec.yaml")); err == nil {
		if strings.Contains(string(b), "sdk: flutter") {
			out = append(out, "flutter")
		} else {
			out = append(out, "dart")
		}
	}
	if isFile(filepath.Join(dir, "supabase", "config.toml")) {
		out = append(out, "supabase")
	}
	if isFile(filepath.Join(dir, "go.mod")) {
		out = append(out, "go")
	}
	if isFile(filepath.Join(dir, "Cargo.toml")) {
		out = append(out, "rust")
	}
	return out
}

// nodeStack classifies a package.json by its dependencies.
// Returns "" if the file is missing or unreadable.
func nodeStack(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var pkg struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if err := json.Unmarshal(b, &pkg); err != nil {
		return "node" // a package.json we can't parse is still a Node project
	}
	has := func(name string) bool {
		_, a := pkg.Dependencies[name]
		_, b := pkg.DevDependencies[name]
		return a || b
	}
	switch {
	case has("@nestjs/core"):
		return "nestjs"
	case has("next"):
		return "nextjs"
	case has("n8n-workflow") || has("n8n-core"):
		return "n8n"
	default:
		return "node"
	}
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// CheckRelFile verifies that rel (relative to root, or absolute) exists and is
// a regular file, and returns it in the form forge stores: relative to root
// when inside it, absolute otherwise.
func CheckRelFile(root, rel string) (string, error) {
	abs := rel
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(root, rel)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", &fs.PathError{Op: "check", Path: abs, Err: errors.New("not a regular file")}
	}
	if r, err := filepath.Rel(root, abs); err == nil && r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return r, nil
	}
	return abs, nil
}

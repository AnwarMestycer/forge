package core

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// write creates files under root; keys are slash paths, values contents.
func write(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDetect(t *testing.T) {
	tests := []struct {
		name        string
		files       map[string]string
		wantStack   []string
		wantCompose string
		wantEnv     string
	}{
		{name: "empty dir"},
		{
			name:      "nestjs in root",
			files:     map[string]string{"package.json": `{"dependencies":{"@nestjs/core":"^11"}}`},
			wantStack: []string{"nestjs"},
		},
		{
			name:      "next as devDependency",
			files:     map[string]string{"package.json": `{"devDependencies":{"next":"15"}}`},
			wantStack: []string{"nextjs"},
		},
		{
			name:      "plain and unparseable package.json are node",
			files:     map[string]string{"package.json": `{}`, "tools/package.json": `not json`},
			wantStack: []string{"node"},
		},
		{
			name:      "flutter vs dart",
			files:     map[string]string{"app/pubspec.yaml": "dependencies:\n  flutter:\n    sdk: flutter\n", "cli/pubspec.yaml": "name: x\n"},
			wantStack: []string{"dart", "flutter"},
		},
		{
			name: "monorepo one level down, compose and env in root",
			files: map[string]string{
				"api/package.json":     `{"dependencies":{"@nestjs/core":"1"}}`,
				"supabase/config.toml": "",
				"docker-compose.yml":   "",
				"compose.yaml":         "", // Docker prefers compose.yaml
				".env":                 "SECRET=x",
			},
			wantStack:   []string{"nestjs", "supabase"},
			wantCompose: "compose.yaml",
			wantEnv:     ".env",
		},
		{
			name: "node_modules, hidden dirs and two levels down are ignored",
			files: map[string]string{
				"node_modules/next/package.json": `{"dependencies":{"next":"1"}}`,
				".cache/go.mod":                  "",
				"a/b/Cargo.toml":                 "",
			},
		},
		{
			name:      "go and rust",
			files:     map[string]string{"go.mod": "", "proxy/Cargo.toml": ""},
			wantStack: []string{"go", "rust"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			write(t, root, tt.files)
			got, err := Detect(root)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got.Stack, tt.wantStack) || got.ComposeFile != tt.wantCompose || got.EnvFile != tt.wantEnv {
				t.Fatalf("got %+v, want stack=%v compose=%q env=%q", got, tt.wantStack, tt.wantCompose, tt.wantEnv)
			}
		})
	}
}

func TestCheckRelFile(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	write(t, root, map[string]string{"deploy/compose.yml": "", "..env": ""})
	write(t, outside, map[string]string{"shared.env": ""})

	if got, err := CheckRelFile(root, "deploy/compose.yml"); err != nil || got != filepath.Join("deploy", "compose.yml") {
		t.Fatalf("relative: %q %v", got, err)
	}
	if got, err := CheckRelFile(root, filepath.Join(root, "deploy/compose.yml")); err != nil || got != filepath.Join("deploy", "compose.yml") {
		t.Fatalf("absolute inside root should become relative: %q %v", got, err)
	}
	if got, err := CheckRelFile(root, filepath.Join(outside, "shared.env")); err != nil || got != filepath.Join(outside, "shared.env") {
		t.Fatalf("outside root should stay absolute: %q %v", got, err)
	}
	if got, err := CheckRelFile(root, filepath.Join(root, "..env")); err != nil || got != "..env" {
		t.Fatalf("a file named ..env is inside root: %q %v", got, err)
	}
	if _, err := CheckRelFile(root, "missing.yml"); err == nil {
		t.Fatal("missing file should fail")
	}
	if _, err := CheckRelFile(root, "deploy"); err == nil {
		t.Fatal("directory should fail")
	}
}

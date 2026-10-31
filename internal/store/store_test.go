package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"slices"
	"testing"
)

func open(t *testing.T) *Store {
	t.Helper()
	s, err := Open(context.Background(), filepath.Join(t.TempDir(), "forge.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestAddAndList(t *testing.T) {
	ctx := context.Background()
	s := open(t)

	if _, err := s.AddProject(ctx, Project{Name: "api", Path: "/work/api"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddProject(ctx, Project{Name: "web", Path: "/work/web"}); err != nil {
		t.Fatal(err)
	}

	got, err := s.ListProjects(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "api" || got[1].Path != "/work/web" || got[0].CreatedAt.IsZero() {
		t.Fatalf("unexpected projects: %+v", got)
	}
}

func TestAddDuplicatePath(t *testing.T) {
	ctx := context.Background()
	s := open(t)

	if _, err := s.AddProject(ctx, Project{Name: "api", Path: "/work/api"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddProject(ctx, Project{Name: "other-name", Path: "/work/api"}); !errors.Is(err, ErrProjectExists) {
		t.Fatalf("want ErrProjectExists, got %v", err)
	}
}

func TestReopenKeepsDataAndSkipsMigrations(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "forge.db")

	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddProject(ctx, Project{Name: "api", Path: "/work/api"}); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()

	s, err = Open(ctx, path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = s.Close() }()
	got, err := s.ListProjects(ctx)
	if err != nil || len(got) != 1 {
		t.Fatalf("after reopen: %v %+v", err, got)
	}
}

func TestProjectFieldsRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := open(t)

	in := Project{Name: "api", Path: "/work/api", Stack: []string{"nestjs", "supabase"}, ComposeFile: "compose.yaml", EnvFile: ".env"}
	if _, err := s.AddProject(ctx, in); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddProject(ctx, Project{Name: "bare", Path: "/work/bare"}); err != nil {
		t.Fatal(err)
	}
	got, err := s.ListProjects(ctx)
	if err != nil {
		t.Fatal(err)
	}
	p := got[0]
	if !slices.Equal(p.Stack, in.Stack) || p.ComposeFile != in.ComposeFile || p.EnvFile != in.EnvFile {
		t.Fatalf("round trip: got %+v", p)
	}
	if got[1].Stack != nil || got[1].ComposeFile != "" || got[1].EnvFile != "" {
		t.Fatalf("empty fields: got %+v", got[1])
	}
}

// A database created by M0 (schema version 1) must upgrade in place.
func TestMigrateFromV1(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "forge.db")

	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		migrations[0],
		`insert into projects (name, path, created_at) values ('old', '/work/old', '2026-10-30T10:00:00Z')`,
		`pragma user_version = 1`,
	} {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	_ = db.Close()

	s, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	defer func() { _ = s.Close() }()
	got, err := s.ListProjects(ctx)
	if err != nil || len(got) != 1 || got[0].Name != "old" || got[0].Stack != nil {
		t.Fatalf("after upgrade: %v %+v", err, got)
	}
	var v int
	if err := s.db.QueryRowContext(ctx, "pragma user_version").Scan(&v); err != nil || v != len(migrations) {
		t.Fatalf("user_version = %d, %v", v, err)
	}
}

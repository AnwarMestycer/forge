package store

import (
	"context"
	"errors"
	"path/filepath"
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

	if _, err := s.AddProject(ctx, "api", "/work/api"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddProject(ctx, "web", "/work/web"); err != nil {
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

	if _, err := s.AddProject(ctx, "api", "/work/api"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddProject(ctx, "other-name", "/work/api"); !errors.Is(err, ErrProjectExists) {
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
	if _, err := s.AddProject(ctx, "api", "/work/api"); err != nil {
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

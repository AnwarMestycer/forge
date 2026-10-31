// Package store is forge's local SQLite database (~/.forge/forge.db).
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// ErrProjectExists is returned when a project with the same path is already registered.
var ErrProjectExists = errors.New("project already registered")

// migrations are applied in order; PRAGMA user_version records how many have run.
// Append only — never edit a migration that has shipped.
var migrations = []string{
	`create table projects (
		id         integer primary key,
		name       text not null,
		path       text not null unique,
		created_at text not null
	)`,
	// M1: what the project is and how to run it. Paths are relative to the
	// project root (or absolute when outside it). stack is comma-separated.
	// env_file is a path only — env contents (secrets) never enter the DB.
	`alter table projects add column stack text not null default '';
	 alter table projects add column compose_file text not null default '';
	 alter table projects add column env_file text not null default ''`,
}

// Store wraps the forge database.
type Store struct {
	db *sql.DB
}

// Project is a directory registered with `forge add`.
type Project struct {
	ID          int64
	Name        string
	Path        string
	Stack       []string // e.g. ["nestjs", "supabase"]
	ComposeFile string   // relative to Path; "" if none
	EnvFile     string   // relative to Path; "" if none
	CreatedAt   time.Time
}

// Open opens (creating if needed) the database at path and applies pending migrations.
func Open(ctx context.Context, path string) (*Store, error) {
	dsn := "file:" + path + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(wal)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	s := &Store{db: db}
	if err := s.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate %s: %w", path, err)
	}
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate(ctx context.Context) error {
	var applied int
	if err := s.db.QueryRowContext(ctx, "pragma user_version").Scan(&applied); err != nil {
		return err
	}
	for i := applied; i < len(migrations); i++ {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, migrations[i]); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
		// PRAGMA does not take bind parameters; i is an int we control.
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("pragma user_version = %d", i+1)); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// AddProject registers p (ID and CreatedAt are filled in). p.Path should
// already be absolute and clean.
func (s *Store) AddProject(ctx context.Context, p Project) (Project, error) {
	p.CreatedAt = time.Now().UTC().Truncate(time.Second)
	res, err := s.db.ExecContext(ctx,
		`insert into projects (name, path, stack, compose_file, env_file, created_at)
		 values (?, ?, ?, ?, ?, ?)`,
		p.Name, p.Path, strings.Join(p.Stack, ","), p.ComposeFile, p.EnvFile, p.CreatedAt.Format(time.RFC3339))
	if err != nil {
		var se *sqlite.Error
		if errors.As(err, &se) && se.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE {
			return Project{}, ErrProjectExists
		}
		return Project{}, err
	}
	if p.ID, err = res.LastInsertId(); err != nil {
		return Project{}, err
	}
	return p, nil
}

// ListProjects returns all projects, oldest first.
func (s *Store) ListProjects(ctx context.Context) ([]Project, error) {
	rows, err := s.db.QueryContext(ctx, `select id, name, path, stack, compose_file, env_file, created_at
		 from projects order by id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var projects []Project
	for rows.Next() {
		var p Project
		var stack, created string
		if err := rows.Scan(&p.ID, &p.Name, &p.Path, &stack, &p.ComposeFile, &p.EnvFile, &created); err != nil {
			return nil, err
		}
		if stack != "" {
			p.Stack = strings.Split(stack, ",")
		}
		if p.CreatedAt, err = time.Parse(time.RFC3339, created); err != nil {
			return nil, fmt.Errorf("project %d: bad created_at %q: %w", p.ID, created, err)
		}
		projects = append(projects, p)
	}
	return projects, rows.Err()
}

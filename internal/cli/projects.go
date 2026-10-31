package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/AnwarMestycer/forge/internal/config"
	"github.com/AnwarMestycer/forge/internal/core"
	"github.com/AnwarMestycer/forge/internal/store"
)

// openStore loads the config and opens the forge database; callers must Close it.
func openStore(cmd *cobra.Command) (*store.Store, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	return store.Open(cmd.Context(), cfg.DBPath)
}

func newAddCmd() *cobra.Command {
	var name, stack, compose, env string
	cmd := &cobra.Command{
		Use:   "add [path]",
		Short: "Register a project with forge (default: current directory)",
		Long: `Register a project with forge (default: current directory).

forge detects the stack (from package.json, pubspec.yaml, go.mod, Cargo.toml,
supabase/config.toml — in the root and one level down), the Docker Compose file
and a .env file. Running add again on a registered project re-detects and
updates it (the name is kept unless --name is given). Flags override detection; --compose and --env are relative to
the project directory. Only the env file's path is stored, never its contents.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "."
			if len(args) == 1 {
				dir = args[0]
			}
			path, err := projectPath(dir)
			if err != nil {
				return err
			}
			p, err := describeProject(path, stack, compose, env)
			if err != nil {
				return err
			}

			s, err := openStore(cmd)
			if err != nil {
				return err
			}
			defer func() { _ = s.Close() }()

			// Re-running add on a registered project refreshes it (re-detects;
			// keeps the name unless --name is given).
			verb := "added"
			p.Name = name
			if p.Name == "" {
				p.Name = filepath.Base(path)
			}
			added, err := s.AddProject(cmd.Context(), p)
			if errors.Is(err, store.ErrProjectExists) {
				verb = "updated"
				p.Name = name
				added, err = s.UpdateProject(cmd.Context(), p)
			}
			if err != nil {
				return err
			}
			p = added
			out := cmd.OutOrStdout()
			_, _ = fmt.Fprintf(out, "%s %s (%s)\n", verb, p.Name, p.Path)
			_, _ = fmt.Fprintf(out, "  stack:   %s\n", orNone(strings.Join(p.Stack, ", ")))
			_, _ = fmt.Fprintf(out, "  compose: %s\n", orNone(p.ComposeFile))
			_, err = fmt.Fprintf(out, "  env:     %s\n", orNone(p.EnvFile))
			return err
		},
	}
	f := cmd.Flags()
	f.StringVar(&name, "name", "", "project name (default: directory name)")
	f.StringVar(&stack, "stack", "", "comma-separated stack, e.g. nestjs,supabase (default: detected)")
	f.StringVar(&compose, "compose", "", "Docker Compose file (default: detected)")
	f.StringVar(&env, "env", "", "env file loaded by `forge switch` (default: .env if present)")
	return cmd
}

// describeProject combines detection with user overrides into a Project
// (Name not set).
func describeProject(path, stack, compose, env string) (store.Project, error) {
	d, err := core.Detect(path)
	if err != nil {
		return store.Project{}, err
	}
	p := store.Project{Path: path, Stack: d.Stack, ComposeFile: d.ComposeFile, EnvFile: d.EnvFile}
	if stack != "" {
		p.Stack = nil
		for _, s := range strings.Split(stack, ",") {
			if s = strings.ToLower(strings.TrimSpace(s)); s != "" && !slices.Contains(p.Stack, s) {
				p.Stack = append(p.Stack, s)
			}
		}
	}
	if compose != "" {
		if p.ComposeFile, err = core.CheckRelFile(path, compose); err != nil {
			return store.Project{}, fmt.Errorf("--compose: %w", err)
		}
	}
	if env != "" {
		if p.EnvFile, err = core.CheckRelFile(path, env); err != nil {
			return store.Project{}, fmt.Errorf("--env: %w", err)
		}
	}
	return p, nil
}

func orNone(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// projectPath resolves dir to a clean absolute path and checks it is a directory.
func projectPath(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	abs, err = filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", abs)
	}
	return abs, nil
}

func newListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List registered projects",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := openStore(cmd)
			if err != nil {
				return err
			}
			defer func() { _ = s.Close() }()

			projects, err := s.ListProjects(cmd.Context())
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(projects) == 0 {
				_, err := fmt.Fprintln(out, "no projects yet — run `forge add .` in a project directory")
				return err
			}
			w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			_, _ = fmt.Fprintln(w, "NAME\tSTACK\tCOMPOSE\tPATH\tADDED")
			for _, p := range projects {
				_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", p.Name, orNone(strings.Join(p.Stack, ",")),
					orNone(p.ComposeFile), p.Path, p.CreatedAt.Local().Format(time.DateOnly))
			}
			return w.Flush()
		},
	}
}

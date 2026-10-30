package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/AnwarMestycer/forge/internal/config"
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
	var name string
	cmd := &cobra.Command{
		Use:   "add [path]",
		Short: "Register a project with forge (default: current directory)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "."
			if len(args) == 1 {
				dir = args[0]
			}
			path, err := projectPath(dir)
			if err != nil {
				return err
			}
			if name == "" {
				name = filepath.Base(path)
			}

			s, err := openStore(cmd)
			if err != nil {
				return err
			}
			defer func() { _ = s.Close() }()

			p, err := s.AddProject(cmd.Context(), name, path)
			if errors.Is(err, store.ErrProjectExists) {
				return fmt.Errorf("%s is already registered (see `forge list`)", path)
			}
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "added %s (%s)\n", p.Name, p.Path)
			return err
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "project name (default: directory name)")
	return cmd
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
			_, _ = fmt.Fprintln(w, "NAME\tPATH\tADDED")
			for _, p := range projects {
				_, _ = fmt.Fprintf(w, "%s\t%s\t%s\n", p.Name, p.Path, p.CreatedAt.Local().Format(time.DateOnly))
			}
			return w.Flush()
		},
	}
}

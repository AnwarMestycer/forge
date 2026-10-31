package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/AnwarMestycer/forge/internal/core"
	"github.com/AnwarMestycer/forge/internal/store"
)

// maxChanged caps how many uncommitted files "where you left off" lists.
const maxChanged = 10

func newSwitchCmd() *cobra.Command {
	var noUp, noShell bool
	cmd := &cobra.Command{
		Use:   "switch <project>",
		Short: "Start a project's containers, load its env and drop into a shell there",
		Long: `Switch to a project: start its containers (compose up -d), load its env file,
show where you left off (branch, uncommitted files, recent commits), then open
a shell in the project directory with the env loaded. Type ` + "`exit`" + ` to leave.

<project> is a name from ` + "`forge list`" + `, or a path.

Why a subshell? forge runs as a child of your shell, and a child process can
never change its parent's directory or environment — so forge starts a new
shell that already has them. $FORGE_PROJECT is set inside it (handy for your prompt).

Compose runs as "docker compose"; set FORGE_COMPOSE="podman compose" to change it.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			out := cmd.OutOrStdout()

			s, err := openStore(cmd)
			if err != nil {
				return err
			}
			defer func() { _ = s.Close() }()

			p, err := findProject(cmd, s, args[0])
			if err != nil {
				return err
			}
			if info, err := os.Stat(p.Path); err != nil || !info.IsDir() {
				return fmt.Errorf("%s: project directory %s is gone (moved? re-run `forge add` at its new path)", p.Name, p.Path)
			}

			var env []string
			if p.EnvFile != "" {
				if env, err = core.LoadEnv(core.ResolvePath(p.Path, p.EnvFile)); err != nil {
					_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: %v — continuing without it\n", err)
				}
			}

			_, _ = fmt.Fprintf(out, "→ %s (%s)\n", p.Name, p.Path)
			if p.ComposeFile != "" && !noUp {
				_, _ = fmt.Fprintf(out, "  starting containers (%s)…\n", p.ComposeFile)
				if err := core.ComposeUp(ctx, p.Path, core.ResolvePath(p.Path, p.ComposeFile), env, out, cmd.ErrOrStderr()); err != nil {
					return err
				}
			}
			if p.EnvFile != "" && env != nil {
				_, _ = fmt.Fprintf(out, "  env: %d vars from %s\n", len(env), p.EnvFile)
			}

			git, isRepo, err := core.ReadGitState(ctx, p.Path)
			if err != nil {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: %v\n", err)
			}
			printLeftOff(out, p, git, isRepo, time.Now())

			if err := s.MarkSwitched(ctx, p.ID, time.Now()); err != nil {
				return err
			}
			if noShell {
				return nil
			}
			if cur := os.Getenv("FORGE_PROJECT"); cur != "" {
				_, _ = fmt.Fprintf(out, "\n(note: you are already in a forge shell for %s — this one nests inside it)\n", cur)
			}
			_, _ = fmt.Fprintf(out, "\nentering %s shell — type `exit` to leave\n", p.Name)
			return core.RunShell(p.Path, p.Name, env)
		},
	}
	cmd.Flags().BoolVar(&noUp, "no-up", false, "don't start containers")
	cmd.Flags().BoolVar(&noShell, "no-shell", false, "don't open a shell; just start containers and print the summary")
	return cmd
}

// findProject resolves arg as a project name, or as a path when it contains
// a path separator or is "." / "..".
func findProject(cmd *cobra.Command, s *store.Store, arg string) (store.Project, error) {
	ctx := cmd.Context()
	if isPathArg(arg) {
		path, err := projectPath(arg)
		if err != nil {
			return store.Project{}, err
		}
		all, err := s.ListProjects(ctx)
		if err != nil {
			return store.Project{}, err
		}
		for _, p := range all {
			if p.Path == path {
				return p, nil
			}
		}
		return store.Project{}, fmt.Errorf("%s is not registered — run `forge add %s` first", path, arg)
	}

	matches, err := s.ProjectsByName(ctx, arg)
	if err != nil {
		return store.Project{}, err
	}
	switch len(matches) {
	case 0:
		return store.Project{}, fmt.Errorf("no project named %q (see `forge list`)", arg)
	case 1:
		return matches[0], nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d projects are named %q — switch by path, or rename one with `forge add <path> --name <new>`:", len(matches), arg)
	for _, p := range matches {
		fmt.Fprintf(&b, "\n  %s", p.Path)
	}
	return store.Project{}, errors.New(b.String())
}

// isPathArg reports whether a project argument is a path rather than a name.
func isPathArg(arg string) bool {
	return arg == "." || arg == ".." || strings.ContainsRune(arg, os.PathSeparator)
}

// printLeftOff prints the "where you left off" block.
func printLeftOff(w io.Writer, p store.Project, git core.GitState, isRepo bool, now time.Time) {
	_, _ = fmt.Fprintln(w, "\nwhere you left off")
	if p.SwitchedAt.IsZero() {
		_, _ = fmt.Fprintln(w, "  last here: first switch")
	} else {
		_, _ = fmt.Fprintf(w, "  last here: %s\n", ago(now.Sub(p.SwitchedAt)))
	}
	if !isRepo {
		_, _ = fmt.Fprintln(w, "  git:       not a git repository")
		return
	}
	_, _ = fmt.Fprintf(w, "  branch:    %s\n", git.Branch)
	if n := len(git.Changed); n == 0 {
		_, _ = fmt.Fprintln(w, "  changes:   clean")
	} else {
		_, _ = fmt.Fprintf(w, "  changes:   %d uncommitted\n", n)
		for i, c := range git.Changed {
			if i == maxChanged {
				_, _ = fmt.Fprintf(w, "             … and %d more\n", n-maxChanged)
				break
			}
			_, _ = fmt.Fprintf(w, "             %s\n", c)
		}
	}
	if len(git.Recent) == 0 {
		_, _ = fmt.Fprintln(w, "  commits:   none yet")
		return
	}
	_, _ = fmt.Fprintln(w, "  recent:")
	for _, c := range git.Recent {
		_, _ = fmt.Fprintf(w, "             %s\n", c)
	}
}

// ago formats a duration as "just now", "5 minutes ago", "3 days ago".
func ago(d time.Duration) string {
	unit := func(n int, s string) string {
		if n == 1 {
			return "1 " + s + " ago"
		}
		return fmt.Sprintf("%d %ss ago", n, s)
	}
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return unit(int(d/time.Minute), "minute")
	case d < 24*time.Hour:
		return unit(int(d/time.Hour), "hour")
	default:
		return unit(int(d/(24*time.Hour)), "day")
	}
}

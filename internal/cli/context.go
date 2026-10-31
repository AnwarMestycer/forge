package cli

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/AnwarMestycer/forge/internal/config"
	"github.com/AnwarMestycer/forge/internal/core"
	"github.com/AnwarMestycer/forge/internal/store"
)

func newContextCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "context",
		Short: "Keep AI coding agents' project context (AGENTS.md, CLAUDE.md) in sync",
	}
	cmd.AddCommand(newContextSyncCmd())
	return cmd
}

func newContextSyncCmd() *cobra.Command {
	var targets string
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "sync [project]",
		Short: "Write project facts (stack, commands, layout, recent work) into agent context files",
		Long: `Write project facts — stack, run/build/test commands, layout, env var names and
recent work — into the instruction files AI coding tools read.

Targets:
  agents  AGENTS.md  read by Codex, Cursor, Copilot's agent, Zed and others
  claude  CLAUDE.md  read by Claude Code; when both are targets it just imports AGENTS.md

forge only edits the block between its <!-- forge:begin --> / <!-- forge:end -->
markers; everything else in the file is yours and is left untouched.

Targets come from --target, else "context.targets" in ~/.forge/config.yaml,
else AGENTS.md plus CLAUDE.md if the project already has one.

[project] is a name from ` + "`forge list`" + ` or a path (default: current directory;
unregistered directories work too).`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			arg := "."
			if len(args) == 1 {
				arg = args[0]
			}
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			s, err := store.Open(cmd.Context(), cfg.DBPath)
			if err != nil {
				return err
			}
			defer func() { _ = s.Close() }()

			p, err := contextProject(cmd, s, arg)
			if err != nil {
				return err
			}

			var chosen []string
			switch {
			case targets != "":
				chosen = strings.Split(targets, ",")
			case len(cfg.ContextTargets) > 0:
				chosen = cfg.ContextTargets
			default:
				chosen = core.DefaultTargets(p.Path)
			}
			if chosen, err = core.NormalizeTargets(chosen); err != nil {
				return err
			}

			facts, err := core.GatherFacts(cmd.Context(), p.Path, p.Name, p.Stack, p.ComposeFile, p.EnvFile)
			if err != nil {
				return err
			}
			results, err := core.SyncContext(p.Path, facts, chosen, !dryRun)
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			for _, r := range results {
				status := "unchanged"
				switch {
				case r.Created:
					status = "created"
				case r.Changed:
					status = "updated"
				}
				if dryRun && r.Changed {
					status = "would be " + status
				}
				_, _ = fmt.Fprintf(out, "%-22s %s\n", r.File, status)
				if dryRun && r.Changed {
					_, _ = fmt.Fprintf(out, "----- %s -----\n%s\n", r.File, r.Content)
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&targets, "target", "", "comma-separated targets: "+strings.Join(core.ValidTargets(), ", "))
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would be written without touching any file")
	return cmd
}

// contextProject resolves arg to a registered project, or — for an
// unregistered directory — a project described by detection alone.
func contextProject(cmd *cobra.Command, s *store.Store, arg string) (store.Project, error) {
	p, err := findProject(cmd, s, arg)
	if err == nil || !isPathArg(arg) {
		return p, err
	}
	path, perr := projectPath(arg)
	if perr != nil {
		return store.Project{}, perr
	}
	p, err = describeProject(path, "", "", "")
	p.Name = filepath.Base(path)
	return p, err
}

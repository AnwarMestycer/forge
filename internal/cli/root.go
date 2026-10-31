// Package cli wires forge's cobra commands.
package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// NewRootCmd builds the `forge` command tree.
func NewRootCmd(version string) *cobra.Command {
	root := &cobra.Command{
		Use:          "forge",
		Short:        "forge — build → verify → break → learn",
		Long:         "forge is a developer command center: scaffold and switch projects,\nvouch for AI-written diffs, and break your app on purpose before production does.",
		Version:      version,
		SilenceUsage: true,
	}
	root.SetVersionTemplate("forge {{.Version}}\n")

	root.AddCommand(
		newAddCmd(),
		newListCmd(),
		newSwitchCmd(),
		newContextCmd(),
		stubCmd("vouch", "Risk report for the current diff (auth/RLS, migrations, env, tests, deps)"),
		stubCmd("chaos", "Run the app behind the fault-injecting proxy"),
	)
	return root
}

// stubCmd is a placeholder for a command that is on the roadmap but not built yet.
func stubCmd(use, short string) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "forge %s: not implemented yet — see ROADMAP.md\n", cmd.Name())
			return err
		},
	}
}

package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func addCompletion(parent *cobra.Command) {
	cmd := &cobra.Command{
		Use:   "completion",
		Short: "Generate autocompletion scripts for various shells",
	}

	cmd.AddCommand(
		addCompletionZsh(),
		addCompletionBash(),
		addCompletionFish(),
		addCompletionPowershell(),
	)

	parent.AddCommand(cmd)
}

func addCompletionZsh() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "zsh",
		Short: "Generates completion scripts for zsh",
		Example: `To load completion run

	. <(hauler completion zsh)

	To configure your zsh shell to load completions for each session add to your zshrc

	# ~/.zshrc or ~/.profile
	command -v hauler >/dev/null && . <(hauler completion zsh)

	or write a cached file in one of the completion directories in your ${fpath}:

	echo "${fpath// /\n}" | grep -i completion
	hauler completion zsh > _hauler

	mv _hauler ~/.oh-my-zsh/completions  # oh-my-zsh
	mv _hauler ~/.zprezto/modules/completion/external/src/  # zprezto`,
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Root().GenZshCompletion(os.Stdout)
			// Cobra doesn't source zsh completion file, explicitly doing it here
			fmt.Println("compdef _hauler hauler")
		},
	}
	return cmd
}

func addCompletionBash() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bash",
		Short: "Generates completion scripts for bash",
		Example: `To load completion run

	. <(hauler completion bash)

	To configure your bash shell to load completions for each session add to your bashrc

	# ~/.bashrc or ~/.profile
	command -v hauler >/dev/null && . <(hauler completion bash)`,
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Root().GenBashCompletion(os.Stdout)
		},
	}
	return cmd
}

func addCompletionFish() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fish",
		Short: "Generates completion scripts for fish",
		Example: `To configure your fish shell to load completions for each session write this script to your completions dir:

	hauler completion fish > ~/.config/fish/completions/hauler.fish

	See http://fishshell.com/docs/current/index.html#completion-own for more details`,
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Root().GenFishCompletion(os.Stdout, true)
		},
	}
	return cmd
}

func addCompletionPowershell() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "powershell",
		Short: "Generates completion scripts for powershell",
		Long:  `The completion sub-command generates completion scripts for powershell.`,
		Example: `To load completion run

	. <(hauler completion powershell)

	To configure your powershell shell to load completions for each session add to your powershell profile

	Windows:

	cd "$env:USERPROFILE\Documents\WindowsPowerShell\Modules"
	hauler completion powershell >> hauler-completion.ps1

	Linux:

	cd "${XDG_CONFIG_HOME:-"$HOME/.config/"}/powershell/modules"
	hauler completion powershell >> hauler-completions.ps1`,
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Root().GenPowerShellCompletion(os.Stdout)
		},
	}
	return cmd
}

package plugin

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli-plugins/manager"
	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/connhelper"
	"github.com/docker/docker/client"
	"github.com/spf13/cobra"
)

func runPlugin(dockerCli *command.DockerCli, plugin *cobra.Command, meta manager.Metadata) error {
	tcmd := newPluginCommand(dockerCli, plugin, meta)

	// Doing this here avoids also calling it for the metadata
	// command which needlessly initializes the client and tries
	// to connect to the daemon.
	plugin.PersistentPreRunE = func(_ *cobra.Command, _ []string) error {
		return tcmd.Initialize(withPluginClientConn(plugin.Name()))
	}

	cmd, _, err := tcmd.HandleGlobalFlags()
	if err != nil {
		return err
	}
	return cmd.Execute()
}

// Run is the top-level entry point to the CLI plugin framework. It should be called from your plugin's `main()` function.
func Run(makeCmd func(command.Cli) *cobra.Command, meta manager.Metadata) {
	dockerCli, err := command.NewDockerCli()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	plugin := makeCmd(dockerCli)

	if err := runPlugin(dockerCli, plugin, meta); err != nil {
		if sterr, ok := err.(cli.StatusError); ok {
			if sterr.Status != "" {
				fmt.Fprintln(dockerCli.Err(), sterr.Status)
			}
			// StatusError should only be used for errors, and all errors should
			// have a non-zero exit status, so never exit with 0
			if sterr.StatusCode == 0 {
				os.Exit(1)
			}
			os.Exit(sterr.StatusCode)
		}
		fmt.Fprintln(dockerCli.Err(), err)
		os.Exit(1)
	}
}

func withPluginClientConn(name string) command.InitializeOpt {
	return command.WithInitializeClient(func(dockerCli *command.DockerCli) (client.APIClient, error) {
		cmd := "docker"
		if x := os.Getenv(manager.ReexecEnvvar); x != "" {
			cmd = x
		}
		var flags []string

		// Accumulate all the global arguments, that is those
		// up to (but not including) the plugin's name. This
		// ensures that `docker system dial-stdio` is
		// evaluating the same set of `--config`, `--tls*` etc
		// global options as the plugin was called with, which
		// in turn is the same as what the original docker
		// invocation was passed.
		for _, a := range os.Args[1:] {
			if a == name {
				break
			}
			flags = append(flags, a)
		}
		flags = append(flags, "system", "dial-stdio")

		helper, err := connhelper.GetCommandConnectionHelper(cmd, flags...)
		if err != nil {
			return nil, err
		}

		return client.NewClientWithOpts(client.WithDialContext(helper.Dialer))
	})
}

func newPluginCommand(dockerCli *command.DockerCli, plugin *cobra.Command, meta manager.Metadata) *cli.TopLevelCommand {
	name := plugin.Name()
	fullname := manager.NamePrefix + name

	cmd := &cobra.Command{
		Use:                   fmt.Sprintf("docker [OPTIONS] %s [ARG...]", name),
		Short:                 fullname + " is a Docker CLI plugin",
		SilenceUsage:          true,
		SilenceErrors:         true,
		TraverseChildren:      true,
		DisableFlagsInUseLine: true,
	}
	opts, flags := cli.SetupPluginRootCommand(cmd)

	cmd.SetOutput(dockerCli.Out())

	cmd.AddCommand(
		plugin,
		newMetadataSubcommand(plugin, meta),
	)

	cli.DisableFlagsInUseLine(cmd)

	return cli.NewTopLevelCommand(cmd, dockerCli, opts, flags)
}

func newMetadataSubcommand(plugin *cobra.Command, meta manager.Metadata) *cobra.Command {
	if meta.ShortDescription == "" {
		meta.ShortDescription = plugin.Short
	}
	cmd := &cobra.Command{
		Use:    manager.MetadataSubcommandName,
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			enc := json.NewEncoder(os.Stdout)
			enc.SetEscapeHTML(false)
			enc.SetIndent("", "     ")
			return enc.Encode(meta)
		},
	}
	return cmd
}

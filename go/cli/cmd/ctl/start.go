package ctl

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	cli "schemaf.local/base/cli"
)

func newStartCmd(ctx *cli.Context) *cobra.Command {
	var nativeMode string
	var skipBuild bool
	var wait bool

	cmd := &cobra.Command{
		Use:   "start <compose-file>",
		Short: "Resolve dependencies and start services",
		Long: `Resolve the dependency graph of a compose file and start all services.

Examples:
  schemaf ctl start example/compose/app.yml
  schemaf ctl start example/compose/app.yml --native backend`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCompose(ctx, args[0], "", nativeMode, skipBuild, wait)
		},
	}

	cmd.Flags().StringVar(&nativeMode, "native", "", "Stop container and run this service natively (prints dev-instructions)")
	cmd.Flags().BoolVar(&skipBuild, "skip-build", false, "Start without rebuilding containers")
	cmd.Flags().BoolVar(&wait, "wait", false, "Wait for services to be healthy before returning")

	return cmd
}

func runCompose(ctx *cli.Context, composeFile, devMode, nativeMode string, skipBuild, wait bool) error {
	files, err := resolveAndPrint(composeFile)
	if err != nil {
		return err
	}

	// Inject PROJECT_NAME from x-schemaf metadata on the entry compose file
	injectProjectEnv(files)

	// Load secrets from ~/.<name>/etc/env (dev: ~/.<name>/dev/etc/env)
	loadProjectEnv(ctx.HomeDir, devMode != "")

	shortMap := ShortNameMap(files)
	allSvcs := AllServices(files)

	// Determine which services run in compose vs excluded
	var includeSvcs []string
	var excludeSvcs []string

	if devMode != "" {
		includeSvcs, err = parseShortNames(devMode, shortMap)
		if err != nil {
			return err
		}
		excludeSvcs = difference(allSvcs, includeSvcs)
	} else {
		includeSvcs = allSvcs
		excludeSvcs = []string{}
	}

	// Handle --native: stop container, run natively
	if nativeMode != "" {
		nativeSvcs, err := parseShortNames(nativeMode, shortMap)
		if err != nil {
			return err
		}
		for _, svc := range nativeSvcs {
			containerName := ContainerName(files, svc)
			stopContainer(containerName)
			schemaf := FindSchemafByService(files, svc)
			if schemaf == nil || schemaf.DevInstructions == "" {
				return fmt.Errorf("no dev-instructions defined for service %q", svc)
			}
			cli.Info("Running %s natively:", svc)
			fmt.Println()
			fmt.Println(schemaf.DevInstructions)
			fmt.Println()
			// Execute in shell
			return runShell(schemaf.DevInstructions, files[len(files)-1].Dir)
		}
		return nil
	}

	// Tear down any existing stack first (clean slate, like run.sh did)
	cli.Info("Cleaning up existing stack...")
	downArgs := buildDockerComposeArgs(files)
	downArgs = append(downArgs, "down")
	_ = runDockerCompose(downArgs) // ignore errors (may not be running)
	fmt.Println()

	// Kill native instances of services that will run in compose
	for _, svc := range includeSvcs {
		schemaf := FindSchemafByService(files, svc)
		runNativeStop(svc, schemaf)
	}

	// Apply env-overrides-when-absent for excluded services
	envOverrides := map[string]string{}
	for _, svc := range excludeSvcs {
		schemaf := FindSchemafByService(files, svc)
		if schemaf != nil {
			for k, v := range schemaf.EnvOverridesWhenAbsent {
				envOverrides[k] = v
			}
		}
	}

	// Build docker compose args
	composeArgs := buildDockerComposeArgs(files)
	composeArgs = append(composeArgs, "up", "-d")

	if wait {
		composeArgs = append(composeArgs, "--wait")
	}

	if skipBuild {
		composeArgs = append(composeArgs, "--no-build")
	}

	// Scale excluded services to 0
	for _, svc := range excludeSvcs {
		composeArgs = append(composeArgs, "--scale", svc+"=0")
	}

	// Set env overrides
	for k, v := range envOverrides {
		os.Setenv(k, v)
	}

	if devMode != "" {
		cli.Info("Dev mode: running only [%s] in Docker", strings.Join(includeSvcs, ", "))
		fmt.Println()
	}

	if err := runDockerCompose(composeArgs); err != nil {
		return fmt.Errorf("docker compose up failed: %w", err)
	}

	cli.Success("Started: %s", strings.Join(includeSvcs, ", "))

	// Print dev instructions for excluded services
	if len(excludeSvcs) > 0 {
		fmt.Println()
		fmt.Println("Run excluded services manually:")
		fmt.Println(strings.Repeat("─", 40))
		for _, svc := range excludeSvcs {
			schemaf := FindSchemafByService(files, svc)
			if schemaf != nil && schemaf.DevInstructions != "" {
				fmt.Printf("\n%s:\n", svc)
				fmt.Println(strings.TrimSpace(schemaf.DevInstructions))
			}
		}
		fmt.Println()
	}

	return nil
}

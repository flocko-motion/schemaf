package codegen

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	cli "atlas.local/base/cli"
	"atlas.local/base/compose"
	"github.com/spf13/cobra"
)

func newComposeCmd(ctx *cli.Context) *cobra.Command {
	var outputPath string

	cmd := &cobra.Command{
		Use:   "compose <compose-file>",
		Short: "Generate a merged compose file",
		Long: `Resolve the dependency graph and export a single canonical merged compose file.

Uses 'docker compose config' to merge and interpolate all files.
Output goes to stdout by default (pipe or redirect as needed).

Examples:
  zeus codegen compose example/compose/app.yml
  zeus codegen compose example/compose/app.yml --output deploy/stack.yml`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			files, err := resolveAndPrintCompose(args[0])
			if err != nil {
				return err
			}

			setupEnv(files, ctx.HomeDir)

			composeArgs := buildDockerComposeArgs(files)
			composeArgs = append(composeArgs, "config", "--format", "yaml")

			merged, err := runDockerComposeCapture(composeArgs)
			if err != nil {
				return fmt.Errorf("docker compose config failed: %w", err)
			}

			if outputPath != "" {
				if err := os.WriteFile(outputPath, []byte(merged), 0644); err != nil {
					return fmt.Errorf("writing output file: %w", err)
				}
				cli.Success("Merged compose written to: %s", outputPath)
			} else {
				fmt.Print(merged)
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&outputPath, "output", "o", "", "Write merged compose to file instead of stdout")

	return cmd
}

func resolveAndPrintCompose(path string) ([]*compose.ComposeFile, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolving path: %w", err)
	}
	if _, err := os.Stat(abs); err != nil {
		return nil, fmt.Errorf("compose file not found: %s", abs)
	}

	files, err := compose.Resolve([]string{abs})
	if err != nil {
		return nil, fmt.Errorf("resolving dependencies: %w", err)
	}

	cli.Info("Resolved %d compose file(s):", len(files))
	for _, cf := range files {
		fmt.Printf("  %s\n", cf.Path)
	}
	fmt.Println()

	return files, nil
}

func buildDockerComposeArgs(files []*compose.ComposeFile) []string {
	var args []string
	for _, cf := range files {
		args = append(args, "-f", cf.Path)
	}
	return args
}

func runDockerComposeCapture(args []string) (string, error) {
	cmd := exec.Command("docker", append([]string{"compose"}, args...)...)
	cmd.Stderr = os.Stderr
	cmd.Env = dockerEnv()
	out, err := cmd.Output()
	return string(out), err
}

// dockerEnv mirrors the ctl compose behavior for WSL2 + Docker Desktop.
func dockerEnv() []string {
	env := os.Environ()

	procVersion, err := os.ReadFile("/proc/version")
	if err != nil || !strings.Contains(strings.ToLower(string(procVersion)), "microsoft") {
		return env
	}

	defaultConfig := filepath.Join(os.Getenv("HOME"), ".docker", "config.json")
	data, err := os.ReadFile(defaultConfig)
	if err != nil || !strings.Contains(string(data), "desktop.exe") {
		return env
	}

	atlasDockerDir, err := cli.EnsureProjectDir("docker")
	if err != nil {
		return env
	}
	cleanConfig := filepath.Join(atlasDockerDir, "config.json")
	if _, err := os.Stat(cleanConfig); os.IsNotExist(err) {
		_ = os.WriteFile(cleanConfig, []byte("{}"), 0644)
	}

	result := make([]string, 0, len(env))
	for _, e := range env {
		if !strings.HasPrefix(e, "DOCKER_CONFIG=") {
			result = append(result, e)
		}
	}
	return append(result, "DOCKER_CONFIG="+atlasDockerDir)
}

func setupEnv(files []*compose.ComposeFile, homeDir string) {
	if len(files) == 0 {
		return
	}
	src := filepath.Join(homeDir, ".env")
	if _, err := os.Stat(src); err != nil {
		cli.Warning("env file not found: %s", src)
		return
	}
	dst := filepath.Join(files[len(files)-1].Dir, ".env")
	_ = os.Remove(dst)
	if err := os.Symlink(src, dst); err != nil {
		cli.Warning("could not symlink .env: %v", err)
	}
}

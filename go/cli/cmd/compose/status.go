package compose

import (
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"time"

	cli "atlas.local/base/cli"
	"github.com/spf13/cobra"
)

func newStatusCmd(ctx *cli.Context) *cobra.Command {
	_ = ctx
	cmd := &cobra.Command{
		Use:   "status <compose-file>",
		Short: "Show health status of all services in a composition",
		Long: `Resolve the dependency graph and check health of all services.

Example:
  atlas compose status atlas-graph/compose/graph-api.yml`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			files, err := resolveAndPrint(args[0])
			if err != nil {
				return err
			}

			// Get running containers via docker compose ps
			composeArgs := buildDockerComposeArgs(files)
			composeArgs = append(composeArgs, "ps", "--format", "table {{.Name}}\t{{.Status}}\t{{.Ports}}")

			fmt.Println("Containers:")
			fmt.Println(strings.Repeat("─", 60))
			_ = runDockerCompose(composeArgs)
			fmt.Println()

			// Health checks
			fmt.Println("Health:")
			fmt.Println(strings.Repeat("─", 60))

			httpClient := &http.Client{Timeout: 3 * time.Second}

			for _, cf := range files {
				for svcName, svc := range cf.Services {
					atlas := svc.Atlas
					if atlas == nil {
						// Fall back to file-level atlas for single-service files
						if len(cf.Services) == 1 {
							atlas = cf.Atlas
						}
					}
					if atlas == nil || atlas.Health == nil {
						continue
					}

					label := svcName
					if atlas.ShortName != "" {
						label = fmt.Sprintf("%s (%s)", svcName, atlas.ShortName)
					}

					switch atlas.Health.Type {
					case "http":
						containerName := svc.ContainerName
						if containerName == "" {
							containerName = svcName
						}
						// Try to find port from docker inspect
						url := guessHTTPURL(files, svcName, atlas.Health.Path)
						resp, err := httpClient.Get(url)
						if err == nil && resp.StatusCode < 400 {
							resp.Body.Close()
							cli.Success("%s  %s", label, url)
						} else {
							if resp != nil {
								resp.Body.Close()
							}
							cli.Error("%s  %s", label, url)
						}

					case "redis_ping":
						containerName := svc.ContainerName
						if containerName == "" {
							containerName = svcName
						}
						out, err := exec.Command("docker", "exec", containerName, "redis-cli", "PING").Output()
						if err == nil && strings.TrimSpace(string(out)) == "PONG" {
							cli.Success("%s", label)
						} else {
							cli.Error("%s", label)
						}
					}
				}
			}

			return nil
		},
	}

	return cmd
}

// guessHTTPURL attempts to build a health URL for a service.
// It reads the first exposed host port from docker inspect, falling back to a
// well-known port pattern.
func guessHTTPURL(files []*ComposeFile, svcName string, path string) string {
	containerName := ContainerName(files, svcName)
	// docker inspect to get host port
	out, err := exec.Command("docker", "inspect",
		"--format", `{{range $p, $conf := .NetworkSettings.Ports}}{{if $conf}}{{(index $conf 0).HostPort}}{{end}}{{end}}`,
		containerName,
	).Output()
	if err == nil && len(strings.TrimSpace(string(out))) > 0 {
		port := strings.TrimSpace(string(out))
		return fmt.Sprintf("http://localhost:%s%s", port, path)
	}
	return fmt.Sprintf("http://localhost%s", path)
}

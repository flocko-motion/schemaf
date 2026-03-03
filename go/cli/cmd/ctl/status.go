package ctl

import (
	"fmt"
	"io"
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
		Use:   "status <compose-file> [service]",
		Short: "Show health status of services in a composition",
		Long: `Resolve the dependency graph and check health of all services.

Example:
  zeus ctl status example/compose/app.yml
  zeus ctl status example/compose/app.yml backend`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			files, err := resolveAndPrint(args[0])
			if err != nil {
				return err
			}

			httpClient := &http.Client{Timeout: 3 * time.Second}
			if len(args) == 2 {
				return runStatusForService(httpClient, files, args[1])
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

			services := AllServices(files)
			shortMap := ShortNameMap(files)
			for _, svcName := range services {
				label := svcName
				for short, name := range shortMap {
					if name == svcName {
						label = fmt.Sprintf("%s (%s)", svcName, short)
						break
					}
				}
				url := guessHTTPURL(files, svcName, "/health")
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

func runStatusForService(httpClient *http.Client, files []*ComposeFile, service string) error {
	shortMap := ShortNameMap(files)
	services := map[string]bool{}
	for _, svc := range AllServices(files) {
		services[svc] = true
	}

	if resolved, ok := shortMap[service]; ok {
		service = resolved
	}
	if !services[service] {
		return fmt.Errorf("unknown service %q", service)
	}

	url := guessHTTPURL(files, service, "/status")
	resp, err := httpClient.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if resp.StatusCode >= 400 {
		cli.Error("%s  %s", service, resp.Status)
	}

	output := strings.TrimSpace(string(body))
	if output == "" {
		fmt.Println(resp.Status)
		return nil
	}
	fmt.Println(output)
	return nil
}

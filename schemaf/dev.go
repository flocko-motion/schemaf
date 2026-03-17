// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package schemaf

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/flocko-motion/schemaf/cli"
)

// devProvider returns the built-in "dev" command for starting development services.
func (a *App) devProvider(_ *cli.Context) []*cobra.Command {
	cmd := &cobra.Command{
		Use:   "dev [services]",
		Short: "Start dev services: db, infrastructure, backend, frontend, all",
		Long: `Start development services. Specify one or more comma-separated services:

  db              Postgres database
  infrastructure  Postgres + project compose services
  backend         Go server on :7000
  frontend        Vite dev server on :7002
  all             All of the above

Examples:
  ./schemaf.sh dev db,backend
  ./schemaf.sh dev all`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			return a.runDev(args[0])
		},
	}
	return []*cobra.Command{cmd}
}

type devServices struct {
	db       bool
	infra    bool
	backend  bool
	frontend bool
}

func parseDevServices(spec string) (devServices, error) {
	var s devServices
	for _, part := range strings.Split(spec, ",") {
		switch strings.TrimSpace(part) {
		case "db":
			s.db = true
		case "infrastructure":
			s.infra = true
		case "backend":
			s.backend = true
		case "frontend":
			s.frontend = true
		case "all":
			s.db = true
			s.infra = true
			s.backend = true
			s.frontend = true
		default:
			return s, fmt.Errorf("unknown dev service: %s (available: db, infrastructure, backend, frontend, all)", part)
		}
	}
	return s, nil
}

func (a *App) runDev(spec string) error {
	svc, err := parseDevServices(spec)
	if err != nil {
		return err
	}

	// Ensure codegen has been run.
	if _, err := os.Stat("gen/compose.gen.yml"); os.IsNotExist(err) {
		fmt.Fprintln(os.Stderr, "gen/ not found — running codegen first...")
		if err := runCmd("go", "run", "github.com/flocko-motion/schemaf/cmd/schemaf", "codegen", "all"); err != nil {
			return fmt.Errorf("codegen: %w", err)
		}
	}

	// Check ports before starting anything.
	if svc.db || svc.infra {
		if err := checkPort(7003, "postgres"); err != nil {
			return err
		}
	}
	if svc.backend {
		if err := checkPort(7000, "backend"); err != nil {
			return err
		}
	}
	if svc.frontend {
		if err := checkPort(7002, "frontend"); err != nil {
			return err
		}
		if _, err := os.Stat("frontend"); os.IsNotExist(err) {
			return fmt.Errorf("frontend/ directory not found — run codegen first")
		}
	}

	compose := []string{"docker", "compose", "-f", "gen/compose.gen.yml", "-f", "gen/compose.dev.yml"}

	// Start Docker services (skip building backend — it runs natively in dev).
	if svc.infra {
		args := append(compose, "up", "--scale", "backend=0", "--no-build", "-d", "--wait")
		if err := runCmd(args...); err != nil {
			return fmt.Errorf("starting infrastructure: %w", err)
		}
	} else if svc.db {
		args := append(compose, "up", "postgres", "-d", "--wait")
		if err := runCmd(args...); err != nil {
			return fmt.Errorf("starting postgres: %w", err)
		}
	}

	// Warn if backend requested but postgres might not be running.
	if svc.backend && !svc.db && !svc.infra {
		args := append(compose, "ps", "postgres", "--status", "running", "-q")
		out, _ := exec.Command(args[0], args[1:]...).Output()
		if len(strings.TrimSpace(string(out))) == 0 {
			fmt.Fprintln(os.Stderr, "WARNING: postgres is not running — backend may fail to connect.")
			fmt.Fprintln(os.Stderr, "         Start it with: ./schemaf.sh dev db")
		}
	}

	// Track background processes for cleanup.
	var bgProcs []*exec.Cmd
	cleanup := func() {
		for _, p := range bgProcs {
			if p.Process != nil {
				_ = p.Process.Signal(syscall.SIGTERM)
			}
		}
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cleanup()
		os.Exit(0)
	}()

	// Start frontend dev server.
	if svc.frontend {
		cmd := exec.Command("npm", "run", "dev")
		cmd.Dir = "frontend"
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("starting frontend: %w", err)
		}
		bgProcs = append(bgProcs, cmd)

		// If running alone (no backend), wait for it.
		if !svc.backend {
			return cmd.Wait()
		}

		// Give Vite a moment — fail fast if it crashes.
		time.Sleep(time.Second)
		if cmd.ProcessState != nil {
			cleanup()
			return fmt.Errorf("frontend dev server failed to start")
		}
	}

	// Start Go server (foreground) — reuse the app's serve() method.
	if svc.backend {
		err := a.serve()
		cleanup()
		return err
	}

	return nil
}

// checkPort verifies a port is free. If busy, asks the user to kill the blocking process
// or stop the Docker container holding it.
func checkPort(port int, service string) error {
	portStr := strconv.Itoa(port)
	ln, err := net.Listen("tcp", ":"+portStr)
	if err == nil {
		ln.Close()
		return nil
	}

	// Check if a Docker container holds the port.
	if container := findDockerContainer(port); container != "" {
		fmt.Fprintf(os.Stderr, "Port %d is in use by Docker container %q — needed for %s.\n", port, container, service)
		fmt.Fprint(os.Stderr, "Stop it? [y/N] ")
		var answer string
		fmt.Scanln(&answer)
		if strings.ToLower(answer) != "y" {
			return fmt.Errorf("port %d is required for %s — aborting", port, service)
		}
		if err := exec.Command("docker", "stop", container).Run(); err != nil {
			return fmt.Errorf("stopping container %s: %w", container, err)
		}
		fmt.Fprintln(os.Stderr, "Stopped.")
		return nil
	}

	// Try to find a native process holding the port.
	pid := findPortPID(port)
	if pid == 0 {
		return fmt.Errorf("port %d is in use (needed for %s) — kill the process manually and retry", port, service)
	}

	procOut, _ := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "comm=").Output()
	proc := strings.TrimSpace(string(procOut))
	if proc == "" {
		proc = "unknown"
	}

	fmt.Fprintf(os.Stderr, "Port %d is in use by %s (PID %d) — needed for %s.\n", port, proc, pid, service)
	fmt.Fprint(os.Stderr, "Kill it? [y/N] ")

	var answer string
	fmt.Scanln(&answer)
	if strings.ToLower(answer) != "y" {
		return fmt.Errorf("port %d is required for %s — aborting", port, service)
	}

	if p, err := os.FindProcess(pid); err == nil {
		_ = p.Signal(syscall.SIGTERM)
		time.Sleep(500 * time.Millisecond)

		// Check if still alive, force kill.
		if _, err := net.DialTimeout("tcp", ":"+portStr, 200*time.Millisecond); err == nil {
			_ = p.Signal(syscall.SIGKILL)
			time.Sleep(300 * time.Millisecond)
		}
	}

	fmt.Fprintln(os.Stderr, "Killed.")
	return nil
}

// findDockerContainer returns the name of a Docker container using the given host port.
func findDockerContainer(port int) string {
	portStr := strconv.Itoa(port)
	out, err := exec.Command("docker", "ps", "--format", "{{.Names}} {{.Ports}}").Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		// Ports look like: 127.0.0.1:7003->5432/tcp or 0.0.0.0:7000->7000/tcp
		if strings.Contains(line, ":"+portStr+"->") {
			return strings.Fields(line)[0]
		}
	}
	return ""
}

// findPortPID returns the PID holding a TCP port, trying multiple methods.
func findPortPID(port int) int {
	portStr := strconv.Itoa(port)

	// Try fuser first (most reliable on Linux/WSL).
	if out, err := exec.Command("fuser", portStr+"/tcp").CombinedOutput(); err == nil {
		for _, field := range strings.Fields(string(out)) {
			if pid, err := strconv.Atoi(field); err == nil {
				return pid
			}
		}
	}

	// Fallback: parse ss output.
	if out, err := exec.Command("ss", "-tlnp", "sport", "=", ":"+portStr).Output(); err == nil {
		// Look for pid=NNNN in the output.
		for _, line := range strings.Split(string(out), "\n") {
			if idx := strings.Index(line, "pid="); idx >= 0 {
				rest := line[idx+4:]
				end := strings.IndexAny(rest, ",) ")
				if end > 0 {
					if pid, err := strconv.Atoi(rest[:end]); err == nil {
						return pid
					}
				}
			}
		}
	}

	// Last resort: lsof (doesn't work on WSL but works on macOS).
	if out, err := exec.Command("lsof", "-ti", ":"+portStr).Output(); err == nil {
		if pid, err := strconv.Atoi(strings.TrimSpace(strings.Split(string(out), "\n")[0])); err == nil {
			return pid
		}
	}

	return 0
}

// runCmd executes a command with stdout/stderr connected to the terminal.
func runCmd(args ...string) error {
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

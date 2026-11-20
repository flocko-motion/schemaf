// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package schemaf

import (
	"context"
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
	"github.com/flocko-motion/schemaf/constants"
	"github.com/flocko-motion/schemaf/db"
	"github.com/flocko-motion/schemaf/files"
)

// devProvider returns the built-in "dev" command for starting development services.
func (a *App) devProvider(_ *cli.Context) []*cobra.Command {
	var resetDB, autoYes bool

	cmd := &cobra.Command{
		Use:   "dev [services]",
		Short: "Start dev services: db, infrastructure, backend, frontend, all",
		Long: fmt.Sprintf(`Start development services. Specify one or more comma-separated services:

  db              Postgres database
  infrastructure  Postgres + project compose services
  backend         Go server on :%d
  frontend        Vite dev server on :%d
  all             All of the above

Ports are those of the default instance; --instance <n> runs a separate dev
stack with all ports shifted by 10 × n, and sticks to the checkout
(INSTALL.md › Parallel dev instances). --instance default switches back.

Examples:
  ./schemaf.sh dev db,backend
  ./schemaf.sh dev all
  ./schemaf.sh dev all --reset-db
  ./schemaf.sh dev all --instance 349`, constants.Port(), constants.FrontendPort()),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			return a.runDev(args[0], resetDB, autoYes)
		},
	}
	cmd.Flags().BoolVar(&resetDB, "reset-db", false, "Drop and recreate the database schema before starting")
	cmd.Flags().BoolVarP(&autoYes, "yes", "y", false, "Auto-confirm prompts (e.g. kill blocking processes)")
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

func (a *App) runDev(spec string, resetDB, autoYes bool) error {
	svc, err := parseDevServices(spec)
	if err != nil {
		return err
	}

	if err := a.setUpInstance(); err != nil {
		return err
	}
	instance := constants.Instance()

	// Fail fast on auth misconfiguration, before starting any containers.
	if svc.backend {
		if _, err := a.identityProvidersFor(os.Getenv); err != nil {
			return err
		}
	}

	// Ensure codegen has been run.
	if _, err := os.Stat("compose.gen.yml"); os.IsNotExist(err) {
		fmt.Fprintln(os.Stderr, "compose.gen.yml not found — running codegen first...")
		if err := runCmd("go", "run", "github.com/flocko-motion/schemaf/cmd/schemaf", "codegen", "all"); err != nil {
			return fmt.Errorf("codegen: %w", err)
		}
	}

	// Check the container runtime before touching any container.
	if err := cli.CheckRuntime(constants.ProjectName(), instance); err != nil {
		return err
	}

	// The generated compose files take the instance's names and Postgres port
	// from the environment (cli.InstanceComposeEnv); set them for every
	// instance, so nothing from the caller's shell leaks in.
	for _, kv := range cli.InstanceComposeEnv(instance, constants.PostgresPort()) {
		k, v, _ := strings.Cut(kv, "=")
		os.Setenv(k, v)
	}

	// compose.gen.yml requires SCHEMAF_IMAGE for the backend service. Dev never
	// starts that service (the backend runs natively, scaled to 0), so a
	// placeholder satisfies compose; it is never pulled or run.
	if os.Getenv("SCHEMAF_IMAGE") == "" {
		os.Setenv("SCHEMAF_IMAGE", constants.ProjectName()+":dev-runs-natively")
	}

	// Stop production compose if running — dev and prod can't coexist. Only
	// the default instance shares a volume and ports with production;
	// instances leave it alone.
	if instance == 0 {
		if err := stopProdIfRunning(); err != nil {
			return err
		}
	}

	// Backend needs all infrastructure, not just postgres.
	if svc.backend && !svc.infra {
		svc.infra = true
	}

	// --reset-db requires the database to be started.
	if resetDB && !svc.db && !svc.infra {
		svc.db = true
	}

	// Check ports before starting anything.
	if svc.db || svc.infra {
		if err := checkPort(constants.PostgresPort(), "postgres", autoYes); err != nil {
			return err
		}
	}
	if svc.backend {
		if err := checkPort(constants.Port(), "backend", autoYes); err != nil {
			return err
		}
	}
	if svc.frontend {
		// Frontend port is exclusively owned by schemaf — always auto-kill stale Vite.
		if err := checkPort(constants.FrontendPort(), "frontend", true); err != nil {
			return err
		}
		if _, err := os.Stat("frontend"); os.IsNotExist(err) {
			return fmt.Errorf("frontend/ directory not found — run codegen first")
		}
	}

	compose := []string{"compose", "-f", "compose.gen.yml", "-f", "compose.dev.gen.yml"}

	// Start containers (skip building backend — it runs natively in dev).
	if svc.infra {
		args := append(compose, "up", "--scale", "backend=0", "--no-build", "-d", "--wait")
		if err := runContainer(args...); err != nil {
			return fmt.Errorf("starting infrastructure: %w", err)
		}
	} else if svc.db {
		args := append(compose, "up", "postgres", "-d", "--wait")
		if err := runContainer(args...); err != nil {
			return fmt.Errorf("starting postgres: %w", err)
		}
	}

	// Reset the database schema if requested.
	if resetDB && a.hasDB {
		fmt.Fprintln(os.Stderr, "Resetting database schema...")
		db.SetDSN(a.dsn())
		if err := db.ResetSchema(context.Background()); err != nil {
			return fmt.Errorf("reset-db: %w", err)
		}
		if err := db.RunMigrations(context.Background()); err != nil {
			return fmt.Errorf("reset-db migrations: %w", err)
		}
		fmt.Fprintln(os.Stderr, "Database reset complete — all migrations re-applied.")
	}

	// Warn if backend requested but postgres might not be running.
	if svc.backend && !svc.db && !svc.infra {
		out, _ := containerOutput(append(compose, "ps", "postgres", "--status", "running", "-q")...)
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
		npmArgs := []string{"run", "dev"}
		if instance != 0 {
			// Overrides the port in the project's vite.config.ts.
			npmArgs = append(npmArgs, "--", "--port", strconv.Itoa(constants.FrontendPort()), "--strictPort")
		}
		cmd := exec.Command("npm", npmArgs...)
		cmd.Dir = "frontend"
		cmd.Env = append(os.Environ(), "NODE_OPTIONS=--max-http-header-size=32768")
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

// setUpInstance applies dev's --instance to the checkout (.schemaf/instance),
// records the instance in ~/.<name>/dev/instances/<n>/instance.toml and prints
// its ports. The instance itself was resolved before the command ran
// (App.resolveInstanceFor). An instance belongs to one checkout.
func (a *App) setUpInstance() error {
	n := constants.Instance()
	if n != 0 {
		took, err := files.ClaimInstance(n, a.projectRoot)
		if err != nil {
			return err
		}
		if took != "" {
			fmt.Fprintf(os.Stderr, "taking over instance %d from %s, which no longer claims it\n", n, took)
		}
	}
	if a.projectRoot != "" {
		if err := files.PersistInstance(a.projectRoot, a.instanceFlag); err != nil {
			return err
		}
	}
	if n != 0 {
		if err := files.WriteInstanceRecord(files.InstanceRecord{
			Instance:     n,
			BackendPort:  constants.Port(),
			VitePort:     constants.FrontendPort(),
			PostgresPort: constants.PostgresPort(),
			Checkout:     a.projectRoot,
		}); err != nil {
			return err
		}
	}
	fmt.Fprintf(os.Stderr, "ports: backend :%d, Vite :%d, Postgres :%d\n",
		constants.Port(), constants.FrontendPort(), constants.PostgresPort())
	return nil
}

// stopProdIfRunning checks if production compose services are running and stops them.
func stopProdIfRunning() error {
	out, err := containerOutput("compose", "-f", "compose.gen.yml", "ps", "-q")
	if err != nil || len(strings.TrimSpace(string(out))) == 0 {
		return nil
	}
	fmt.Fprintln(os.Stderr, "Stopping production services before dev start...")
	if err := runContainer("compose", "-f", "compose.gen.yml", "down"); err != nil {
		return fmt.Errorf("stopping production services: %w", err)
	}
	fmt.Fprintln(os.Stderr, "Production services stopped.")
	return nil
}

// checkPort verifies a port is free. If busy, asks the user to kill the blocking process
// or stop the container holding it. With autoYes, kills without prompting.
func checkPort(port int, service string, autoYes bool) error {
	portStr := strconv.Itoa(port)

	// Check if any process holds the port. net.Listen is unreliable on macOS
	// (SO_REUSEADDR allows rebinding occupied ports), so we check for actual
	// processes first via the container runtime and OS tools, then fall back to net.Listen.
	hasContainerConflict := findContainer(port) != ""
	hasProcessConflict := findPortPID(port) != 0
	if !hasContainerConflict && !hasProcessConflict {
		// No process found — double-check with net.Listen as a safety net.
		ln, err := net.Listen("tcp", ":"+portStr)
		if err == nil {
			ln.Close()
			return nil
		}
	}

	// Check if a container holds the port.
	if container := findContainer(port); container != "" {
		fmt.Fprintf(os.Stderr, "Port %d is in use by container %q — needed for %s.\n", port, container, service)
		answer := "y"
		if !autoYes {
			fmt.Fprint(os.Stderr, "Stop it? [y/N/c(ontinue)] ")
			fmt.Scanln(&answer)
		}
		switch strings.ToLower(answer) {
		case "c":
			return nil
		case "y":
			if _, err := containerOutput("stop", container); err != nil {
				return fmt.Errorf("stopping container %s: %w", container, err)
			}
			fmt.Fprintln(os.Stderr, "Stopped.")
			return nil
		default:
			return fmt.Errorf("port %d is required for %s — aborting", port, service)
		}
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
	answer := "y"
	if !autoYes {
		fmt.Fprint(os.Stderr, "Kill it? [y/N/c(ontinue)] ")
		fmt.Scanln(&answer)
	}
	switch strings.ToLower(answer) {
	case "c":
		return nil
	case "y":
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
	default:
		return fmt.Errorf("port %d is required for %s — aborting", port, service)
	}
}

// findContainer returns the name of a container using the given host port.
func findContainer(port int) string {
	portStr := strconv.Itoa(port)
	out, err := containerOutput("ps", "--format", "{{.Names}} {{.Ports}}")
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

// runContainer runs a container command (cli.Container) with stdout/stderr
// connected to the terminal.
func runContainer(args ...string) error {
	cmd, err := cli.Container(args...)
	if err != nil {
		return err
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// containerOutput runs a container command (cli.Container) and returns its stdout.
func containerOutput(args ...string) ([]byte, error) {
	cmd, err := cli.Container(args...)
	if err != nil {
		return nil, err
	}
	return cmd.Output()
}

// runCmd executes a command with stdout/stderr connected to the terminal.
func runCmd(args ...string) error {
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

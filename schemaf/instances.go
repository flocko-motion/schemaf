// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package schemaf

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/flocko-motion/schemaf/api"
	"github.com/flocko-motion/schemaf/cli"
	"github.com/flocko-motion/schemaf/constants"
	"github.com/flocko-motion/schemaf/files"
)

// instancesProvider returns the built-in "instances" command: list and remove
// the project's parallel dev instances (INSTALL.md › Parallel dev instances).
func (a *App) instancesProvider(_ *cli.Context) []*cobra.Command {
	cmd := &cobra.Command{
		Use:   "instances",
		Short: "List the project's dev instances on this machine",
		Long: `List every dev instance of the project on this machine, the default one
included: its name, whether its containers run, its ports (backend, Vite,
Postgres) and its checkout.

Start an instance with ./schemaf.sh dev all --instance <n>; remove one with
./schemaf.sh instances rm <n>.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireDevMode("instances"); err != nil {
				return err
			}
			return listInstances(os.Stdout)
		},
	}

	var yes bool
	rm := &cobra.Command{
		Use:   "rm <n>",
		Short: "Remove a dev instance: its containers, database volume, data and record",
		Long: `Stop instance n's containers and delete its Postgres volume, its data
directory and its record (~/.<name>/dev/instances/<n>/). Asks for
confirmation unless -y is given. The default instance can't be removed.
The checkout is left alone.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireDevMode("instances rm"); err != nil {
				return err
			}
			n, err := constants.ParseInstance(args[0])
			if err != nil {
				return err
			}
			if n == 0 {
				return fmt.Errorf("the default instance can't be removed — instances rm takes an instance number")
			}
			return removeInstance(n, yes, os.Stdin)
		},
	}
	rm.Flags().BoolVarP(&yes, "yes", "y", false, "don't ask for confirmation")
	cmd.AddCommand(rm)
	return []*cobra.Command{cmd}
}

func requireDevMode(what string) error {
	if !api.DevMode() {
		return fmt.Errorf("%s is dev-only: run it as ./schemaf.sh %s, which sets SCHEMAF_DEV=1", what, what)
	}
	return nil
}

// composeLabel selects everything compose created for the instance's project.
func composeLabel(n int) string {
	return "label=com.docker.compose.project=" + constants.StackNameFor(n)
}

// containerIDs returns the IDs the runtime lists for args (one per line).
func containerIDs(args ...string) ([]string, error) {
	out, err := containerOutput(args...)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", strings.Join(args, " "), err)
	}
	return strings.Fields(string(out)), nil
}

func listInstances(w io.Writer) error {
	recs, err := files.InstanceRecords()
	if err != nil {
		return err
	}
	// The default instance has no record; any checkout without
	// .schemaf/instance uses it.
	rows := append([]files.InstanceRecord{{}}, recs...)

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "INSTANCE\tRUNNING\tBACKEND\tVITE\tPOSTGRES\tCHECKOUT")
	for _, r := range rows {
		ids, err := containerIDs("ps", "-q", "--filter", composeLabel(r.Instance))
		if err != nil {
			return err
		}
		running := "no"
		if len(ids) > 0 {
			running = "yes"
		}
		// The default instance shares its compose project with production;
		// only production runs the backend in a container.
		if r.Instance == 0 && len(ids) > 0 && productionRunning() {
			running = "production"
		}
		fmt.Fprintln(tw, instanceRow(r, constants.ConfigPort(), running))
	}
	return tw.Flush()
}

// instanceRow is one tab-separated row of `instances` for record r: the ports
// dev would use now, calculated from port (schemaf.toml), not the recorded
// ones. A record whose ports differ (port changed since) is marked.
func instanceRow(r files.InstanceRecord, port int, running string) string {
	checkout := r.Checkout
	if checkout == "" {
		checkout = "-"
	}
	name := constants.InstanceName(r.Instance)
	base, err := constants.InstanceBasePort(port, r.Instance)
	if err != nil {
		return fmt.Sprintf("%s\t%s\t-\t-\t-\t%s  (base port %d is above %d with port = %d; remove it: instances rm %d)",
			name, running, checkout, port+constants.InstancePortStep*r.Instance, constants.MaxInstanceBasePort, port, r.Instance)
	}
	row := fmt.Sprintf("%s\t%s\t%d\t%d\t%d\t%s", name, running, base, base+2, base+3, checkout)
	if r.Instance != 0 && (r.BackendPort != base || r.VitePort != base+2 || r.PostgresPort != base+3) {
		row += fmt.Sprintf("  (recorded ports %d/%d/%d; the next dev updates the record)", r.BackendPort, r.VitePort, r.PostgresPort)
	}
	return row
}

// productionRunning reports whether the production backend container runs
// (dev runs the backend natively, never as <name>-backend).
func productionRunning() bool {
	out, err := containerOutput("container", "inspect", "--format", "{{.State.Running}}", constants.ProjectName()+"-backend")
	return err == nil && strings.TrimSpace(string(out)) == "true"
}

func removeInstance(n int, yes bool, stdin io.Reader) error {
	stack := constants.StackNameFor(n)
	volume := cli.PostgresVolume(constants.ProjectName(), n)
	dir := files.InstanceDir(n)

	_, hasRecord, err := files.ReadInstanceRecord(n)
	if err != nil {
		return err
	}
	_, dirErr := os.Stat(dir)
	containers, err := containerIDs("ps", "-aq", "--filter", composeLabel(n))
	if err != nil {
		return err
	}
	volumes, err := containerIDs("volume", "ls", "-q", "--filter", composeLabel(n))
	if err != nil {
		return err
	}
	if !contains(volumes, volume) {
		if _, err := containerOutput("volume", "inspect", volume); err == nil {
			volumes = append(volumes, volume)
		}
	}
	networks, err := containerIDs("network", "ls", "-q", "--filter", composeLabel(n))
	if err != nil {
		return err
	}
	if !hasRecord && dirErr != nil && len(containers) == 0 && len(volumes) == 0 && len(networks) == 0 {
		return fmt.Errorf("there is no instance %d (no record in %s, no containers, volumes or networks of %s)", n, dir, stack)
	}

	if !yes {
		fmt.Fprintf(os.Stderr, "Remove instance %d? This stops its containers (%d) and deletes its data for good:\n", n, len(containers))
		for _, v := range volumes {
			fmt.Fprintf(os.Stderr, "  volume %s\n", v)
		}
		fmt.Fprintf(os.Stderr, "  %s\n", dir)
		fmt.Fprint(os.Stderr, "[y/N] ")
		answer, _ := bufio.NewReader(stdin).ReadString('\n')
		if strings.ToLower(strings.TrimSpace(answer)) != "y" {
			return fmt.Errorf("aborted: instance %d left as it is", n)
		}
	}

	if len(containers) > 0 {
		if _, err := containerOutput(append([]string{"rm", "-f"}, containers...)...); err != nil {
			return fmt.Errorf("removing the containers of instance %d: %w", n, err)
		}
	}
	for _, v := range volumes {
		if _, err := containerOutput("volume", "rm", v); err != nil {
			return fmt.Errorf("removing volume %s: %w", v, err)
		}
	}
	for _, id := range networks {
		if _, err := containerOutput("network", "rm", id); err != nil {
			return fmt.Errorf("removing network %s: %w", id, err)
		}
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("removing %s: %w", dir, err)
	}
	fmt.Fprintf(os.Stderr, "Removed instance %d: %d container(s), %d volume(s), %d network(s), %s\n",
		n, len(containers), len(volumes), len(networks), dir)
	return nil
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

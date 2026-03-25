// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package db

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

func backupCmd() *cobra.Command {
	var localPath string

	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Create a database backup",
		Long: `Creates a compressed pg_dump backup of the project database.

By default, uploads to the configured SFTP server (requires BACKUP_SSH_HOST,
BACKUP_SSH_USER, BACKUP_SSH_KEY environment variables).

Use --local to save to a local file instead.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			dsn := dsnValue
			if dsn == "" {
				return fmt.Errorf("database not configured")
			}

			if localPath != "" {
				return runLocalBackup(cmd, dsn, localPath)
			}
			return runSFTPBackup(cmd, dsn)
		},
	}
	cmd.Flags().StringVar(&localPath, "local", "", "save backup to local file path")
	return cmd
}

func restoreCmd() *cobra.Command {
	var localPath string
	var latest bool

	cmd := &cobra.Command{
		Use:   "restore [filename]",
		Short: "Restore a database backup",
		Long: `Restores a database from a compressed pg_dump backup.

With no arguments, lists available remote backups.
With a filename argument, restores that specific backup from SFTP.
Use --latest to restore the most recent backup.
Use --local to restore from a local file.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dsn := dsnValue
			if dsn == "" {
				return fmt.Errorf("database not configured")
			}

			if localPath != "" {
				return runLocalRestore(cmd, dsn, localPath)
			}

			cfg, ok := ReadSFTPConfigFromEnv()
			if !ok {
				return fmt.Errorf("SFTP not configured — set BACKUP_SSH_HOST, BACKUP_SSH_USER, BACKUP_SSH_KEY")
			}

			if latest {
				return runLatestRestore(cmd, dsn, cfg)
			}

			if len(args) == 1 {
				return runSFTPRestore(cmd, dsn, cfg, args[0])
			}

			return runListBackups(cfg)
		},
	}
	cmd.Flags().StringVar(&localPath, "local", "", "restore from local file path")
	cmd.Flags().BoolVar(&latest, "latest", false, "restore the most recent backup")
	return cmd
}

func runLocalBackup(cmd *cobra.Command, dsn, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("creating %s: %w", path, err)
	}
	defer f.Close()

	if err := Backup(cmd.Context(), dsn, f); err != nil {
		os.Remove(path)
		return err
	}

	info, _ := f.Stat()
	fmt.Fprintf(os.Stderr, "Backup saved to %s (%d bytes)\n", path, info.Size())
	return nil
}

func runSFTPBackup(cmd *cobra.Command, dsn string) error {
	cfg, ok := ReadSFTPConfigFromEnv()
	if !ok {
		return fmt.Errorf("SFTP not configured — set BACKUP_SSH_HOST, BACKUP_SSH_USER, BACKUP_SSH_KEY")
	}

	filename, err := BackupToSFTP(cmd.Context(), dsn, cfg)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Backup uploaded: %s\n", filename)

	if err := DeleteOldBackups(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: retention cleanup failed: %v\n", err)
	}
	return nil
}

func runListBackups(cfg SFTPConfig) error {
	backups, err := ListRemoteBackups(cfg)
	if err != nil {
		return err
	}
	if len(backups) == 0 {
		fmt.Fprintln(os.Stderr, "No backups found.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tSIZE\tDATE")
	fmt.Fprintln(w, "────\t────\t────")
	for _, b := range backups {
		size := formatSize(b.Size)
		fmt.Fprintf(w, "%s\t%s\t%s\n", b.Name, size, b.ModTime.Format("2006-01-02 15:04"))
	}
	w.Flush()
	fmt.Fprintf(os.Stderr, "(%d backups)\n", len(backups))
	return nil
}

func runLatestRestore(cmd *cobra.Command, dsn string, cfg SFTPConfig) error {
	backups, err := ListRemoteBackups(cfg)
	if err != nil {
		return err
	}
	if len(backups) == 0 {
		return fmt.Errorf("no backups found")
	}
	return runSFTPRestore(cmd, dsn, cfg, backups[0].Name)
}

func runSFTPRestore(cmd *cobra.Command, dsn string, cfg SFTPConfig, filename string) error {
	fmt.Fprintf(os.Stderr, "Restoring from %s...\n", filename)
	if err := RestoreFromSFTP(cmd.Context(), dsn, cfg, filename); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "Restore complete.")
	return nil
}

func runLocalRestore(cmd *cobra.Command, dsn, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("opening %s: %w", path, err)
	}
	defer f.Close()

	fmt.Fprintf(os.Stderr, "Restoring from %s...\n", path)
	if err := Restore(cmd.Context(), dsn, f); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "Restore complete.")
	return nil
}

func formatSize(bytes int64) string {
	switch {
	case bytes >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(1<<30))
	case bytes >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(1<<20))
	case bytes >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

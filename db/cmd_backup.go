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
	fmt.Fprintf(os.Stderr, "→ Creating local backup to %s\n", path)
	fmt.Fprintf(os.Stderr, "  DSN: %s\n", redactDSN(dsn))

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("creating %s: %w", path, err)
	}
	defer f.Close()

	fmt.Fprintf(os.Stderr, "  Running pg_dump...\n")
	if err := Backup(cmd.Context(), dsn, f); err != nil {
		os.Remove(path)
		return err
	}

	info, _ := f.Stat()
	fmt.Fprintf(os.Stderr, "✓ Backup saved to %s (%s)\n", path, formatSize(info.Size()))
	return nil
}

func runSFTPBackup(cmd *cobra.Command, dsn string) error {
	fmt.Fprintf(os.Stderr, "→ Starting SFTP backup\n")
	fmt.Fprintf(os.Stderr, "  DSN: %s\n", redactDSN(dsn))

	fmt.Fprintf(os.Stderr, "  Reading SFTP config from environment...\n")
	cfg, ok := ReadSFTPConfigFromEnv()
	if !ok {
		return fmt.Errorf("SFTP not configured — set BACKUP_SSH_HOST, BACKUP_SSH_USER, BACKUP_SSH_KEY")
	}
	fmt.Fprintf(os.Stderr, "  Host: %s:%d\n", cfg.Host, cfg.Port)
	fmt.Fprintf(os.Stderr, "  User: %s\n", cfg.User)
	fmt.Fprintf(os.Stderr, "  Path: %s\n", cfg.Path)
	fmt.Fprintf(os.Stderr, "  Retain: %d backups\n", cfg.Retain)
	fmt.Fprintf(os.Stderr, "  Key: %d bytes loaded\n", len(cfg.Key))

	fmt.Fprintf(os.Stderr, "  Connecting to SFTP server...\n")
	filename, err := BackupToSFTP(cmd.Context(), dsn, cfg)
	recordBackup(cmd.Context(), err)
	if err != nil {
		fmt.Fprintf(os.Stderr, "✗ Backup failed: %v\n", err)
		return err
	}
	fmt.Fprintf(os.Stderr, "✓ Backup uploaded: %s\n", filename)

	fmt.Fprintf(os.Stderr, "  Running retention cleanup (keep %d)...\n", cfg.Retain)
	if err := DeleteOldBackups(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "  Warning: retention cleanup failed: %v\n", err)
	} else {
		fmt.Fprintf(os.Stderr, "✓ Retention cleanup done\n")
	}
	return nil
}

func runListBackups(cfg SFTPConfig) error {
	fmt.Fprintf(os.Stderr, "→ Listing backups on %s:%d%s\n", cfg.Host, cfg.Port, cfg.Path)

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
	fmt.Fprintf(os.Stderr, "→ Finding latest backup...\n")
	backups, err := ListRemoteBackups(cfg)
	if err != nil {
		return err
	}
	if len(backups) == 0 {
		return fmt.Errorf("no backups found")
	}
	fmt.Fprintf(os.Stderr, "  Latest: %s (%s)\n", backups[0].Name, formatSize(backups[0].Size))
	return runSFTPRestore(cmd, dsn, cfg, backups[0].Name)
}

func runSFTPRestore(cmd *cobra.Command, dsn string, cfg SFTPConfig, filename string) error {
	fmt.Fprintf(os.Stderr, "→ Restoring from %s\n", filename)
	fmt.Fprintf(os.Stderr, "  DSN: %s\n", redactDSN(dsn))
	fmt.Fprintf(os.Stderr, "  Downloading and restoring...\n")
	if err := RestoreFromSFTP(cmd.Context(), dsn, cfg, filename); err != nil {
		fmt.Fprintf(os.Stderr, "✗ Restore failed: %v\n", err)
		return err
	}
	fmt.Fprintf(os.Stderr, "✓ Restore complete\n")
	return nil
}

func runLocalRestore(cmd *cobra.Command, dsn, path string) error {
	fmt.Fprintf(os.Stderr, "→ Restoring from local file %s\n", path)
	fmt.Fprintf(os.Stderr, "  DSN: %s\n", redactDSN(dsn))

	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("opening %s: %w", path, err)
	}
	defer f.Close()

	fmt.Fprintf(os.Stderr, "  Running psql restore...\n")
	if err := Restore(cmd.Context(), dsn, f); err != nil {
		fmt.Fprintf(os.Stderr, "✗ Restore failed: %v\n", err)
		return err
	}
	fmt.Fprintf(os.Stderr, "✓ Restore complete\n")
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

// redactDSN masks the password in a DSN for safe logging.
func redactDSN(dsn string) string {
	h, p, u, _, db, err := parseDSN(dsn)
	if err != nil {
		return "<invalid DSN>"
	}
	return fmt.Sprintf("postgres://%s:***@%s:%s/%s", u, h, p, db)
}

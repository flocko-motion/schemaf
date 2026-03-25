// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package db

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	slog "github.com/flocko-motion/schemaf/log"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// SFTPConfig holds connection details for remote backup storage.
type SFTPConfig struct {
	Host   string
	Port   int
	User   string
	Key    []byte // PEM-encoded SSH private key
	Path   string // remote directory
	Retain int    // number of backups to keep
	Hour   int    // UTC hour for daily auto-backup
}

// BackupInfo describes a remote backup file.
type BackupInfo struct {
	Name    string
	Size    int64
	ModTime time.Time
}

// ReadSFTPConfigFromEnv reads backup configuration from environment variables.
// Returns false if BACKUP_SSH_HOST is not set (backups not configured).
func ReadSFTPConfigFromEnv() (SFTPConfig, bool) {
	host := os.Getenv("BACKUP_SSH_HOST")
	if host == "" {
		return SFTPConfig{}, false
	}

	port := 22
	if p := os.Getenv("BACKUP_SSH_PORT"); p != "" {
		if v, err := strconv.Atoi(p); err == nil {
			port = v
		}
	}

	keyPath := os.Getenv("BACKUP_SSH_KEY")
	if keyPath == "" {
		slog.Error("BACKUP_SSH_HOST set but BACKUP_SSH_KEY missing")
		return SFTPConfig{}, false
	}
	keyData, err := os.ReadFile(keyPath)
	if err != nil {
		slog.Error("reading SSH key", "path", keyPath, "error", err)
		return SFTPConfig{}, false
	}

	remotePath := os.Getenv("BACKUP_PATH")
	if remotePath == "" {
		remotePath = "/backups"
	}

	retain := 30
	if r := os.Getenv("BACKUP_RETAIN"); r != "" {
		if v, err := strconv.Atoi(r); err == nil && v > 0 {
			retain = v
		}
	}

	hour := 3
	if h := os.Getenv("BACKUP_HOUR"); h != "" {
		if v, err := strconv.Atoi(h); err == nil && v >= 0 && v < 24 {
			hour = v
		}
	}

	user := os.Getenv("BACKUP_SSH_USER")
	if user == "" {
		slog.Error("BACKUP_SSH_HOST set but BACKUP_SSH_USER missing")
		return SFTPConfig{}, false
	}

	return SFTPConfig{
		Host:   host,
		Port:   port,
		User:   user,
		Key:    keyData,
		Path:   remotePath,
		Retain: retain,
		Hour:   hour,
	}, true
}

// Backup runs pg_dump and writes gzip-compressed SQL to w.
func Backup(ctx context.Context, dsn string, w io.Writer) error {
	host, port, user, pass, dbname, err := parseDSN(dsn)
	if err != nil {
		return err
	}

	cmd := exec.CommandContext(ctx, "pg_dump",
		"-h", host, "-p", port, "-U", user, "-d", dbname,
		"--no-password",
	)
	cmd.Env = append(os.Environ(), "PGPASSWORD="+pass)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("pg_dump stdout pipe: %w", err)
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting pg_dump: %w", err)
	}

	gz := gzip.NewWriter(w)
	if _, err := io.Copy(gz, stdout); err != nil {
		cmd.Wait()
		return fmt.Errorf("compressing backup: %w", err)
	}
	if err := gz.Close(); err != nil {
		cmd.Wait()
		return fmt.Errorf("closing gzip: %w", err)
	}

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("pg_dump failed: %w", err)
	}
	return nil
}

// Restore reads gzip-compressed SQL from r and pipes it into psql.
func Restore(ctx context.Context, dsn string, r io.Reader) error {
	host, port, user, pass, dbname, err := parseDSN(dsn)
	if err != nil {
		return err
	}

	gz, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("opening gzip: %w", err)
	}
	defer gz.Close()

	cmd := exec.CommandContext(ctx, "psql",
		"-h", host, "-p", port, "-U", user, "-d", dbname,
		"--no-password", "-q",
	)
	cmd.Env = append(os.Environ(), "PGPASSWORD="+pass)
	cmd.Stdin = gz
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("psql restore failed: %w", err)
	}
	return nil
}

// BackupToSFTP creates a backup and uploads it to the configured SFTP server.
// Returns the remote filename.
func BackupToSFTP(ctx context.Context, dsn string, cfg SFTPConfig) (string, error) {
	client, err := dialSFTP(cfg)
	if err != nil {
		return "", err
	}
	defer client.Close()

	// Ensure remote directory exists.
	client.MkdirAll(cfg.Path)

	project := projectFromDSN(dsn)
	filename := fmt.Sprintf("%s-%s.sql.gz", project, time.Now().UTC().Format("2006-01-02_15-04-05"))
	remotePath := path.Join(cfg.Path, filename)

	f, err := client.Create(remotePath)
	if err != nil {
		return "", fmt.Errorf("creating remote file %s: %w", remotePath, err)
	}
	defer f.Close()

	if err := Backup(ctx, dsn, f); err != nil {
		return "", err
	}

	return filename, nil
}

// RestoreFromSFTP downloads a backup from SFTP and restores it.
func RestoreFromSFTP(ctx context.Context, dsn string, cfg SFTPConfig, filename string) error {
	client, err := dialSFTP(cfg)
	if err != nil {
		return err
	}
	defer client.Close()

	remotePath := path.Join(cfg.Path, filename)
	f, err := client.Open(remotePath)
	if err != nil {
		return fmt.Errorf("opening remote file %s: %w", remotePath, err)
	}
	defer f.Close()

	return Restore(ctx, dsn, f)
}

// ListRemoteBackups returns backup files on the SFTP server, newest first.
func ListRemoteBackups(cfg SFTPConfig) ([]BackupInfo, error) {
	client, err := dialSFTP(cfg)
	if err != nil {
		return nil, err
	}
	defer client.Close()

	entries, err := client.ReadDir(cfg.Path)
	if err != nil {
		return nil, fmt.Errorf("listing %s: %w", cfg.Path, err)
	}

	var backups []BackupInfo
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql.gz") {
			continue
		}
		backups = append(backups, BackupInfo{
			Name:    e.Name(),
			Size:    e.Size(),
			ModTime: e.ModTime(),
		})
	}

	sort.Slice(backups, func(i, j int) bool {
		return backups[i].ModTime.After(backups[j].ModTime)
	})
	return backups, nil
}

// DeleteOldBackups removes the oldest backups beyond the retain count.
func DeleteOldBackups(cfg SFTPConfig) error {
	backups, err := ListRemoteBackups(cfg)
	if err != nil {
		return err
	}
	if len(backups) <= cfg.Retain {
		return nil
	}

	client, err := dialSFTP(cfg)
	if err != nil {
		return err
	}
	defer client.Close()

	for _, b := range backups[cfg.Retain:] {
		remotePath := path.Join(cfg.Path, b.Name)
		if err := client.Remove(remotePath); err != nil {
			slog.Error("deleting old backup", "file", b.Name, "error", err)
		} else {
			slog.Info("deleted old backup", "file", b.Name)
		}
	}
	return nil
}

// RunBackupScheduler runs daily backups at the configured UTC hour.
// It blocks until the context is cancelled.
func RunBackupScheduler(ctx context.Context, dsn string, cfg SFTPConfig) {
	slog.Info("backup scheduler started", "hour", cfg.Hour, "retain", cfg.Retain, "host", cfg.Host)

	for {
		next := nextRunTime(cfg.Hour)
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Until(next)):
		}

		slog.Info("starting scheduled backup")
		filename, err := BackupToSFTP(ctx, dsn, cfg)
		if err != nil {
			slog.Error("scheduled backup failed", "error", err)
			continue
		}
		slog.Info("scheduled backup complete", "file", filename)

		if err := DeleteOldBackups(cfg); err != nil {
			slog.Error("backup retention cleanup failed", "error", err)
		}
	}
}

// nextRunTime returns the next occurrence of the given UTC hour.
func nextRunTime(hour int) time.Time {
	now := time.Now().UTC()
	next := time.Date(now.Year(), now.Month(), now.Day(), hour, 0, 0, 0, time.UTC)
	if !next.After(now) {
		next = next.Add(24 * time.Hour)
	}
	return next
}

// dialSFTP establishes an SFTP connection.
func dialSFTP(cfg SFTPConfig) (*sftp.Client, error) {
	signer, err := ssh.ParsePrivateKey(cfg.Key)
	if err != nil {
		return nil, fmt.Errorf("parsing SSH key: %w", err)
	}

	sshCfg := &ssh.ClientConfig{
		User:            cfg.User,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         30 * time.Second,
	}

	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	sshConn, err := ssh.Dial("tcp", addr, sshCfg)
	if err != nil {
		return nil, fmt.Errorf("SSH connect to %s: %w", addr, err)
	}

	client, err := sftp.NewClient(sshConn)
	if err != nil {
		sshConn.Close()
		return nil, fmt.Errorf("SFTP session: %w", err)
	}
	return client, nil
}

// parseDSN extracts host, port, user, password, dbname from a postgres:// DSN.
func parseDSN(dsn string) (host, port, user, pass, dbname string, err error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return "", "", "", "", "", fmt.Errorf("parsing DSN: %w", err)
	}
	host = u.Hostname()
	port = u.Port()
	if port == "" {
		port = "5432"
	}
	user = u.User.Username()
	pass, _ = u.User.Password()
	dbname = strings.TrimPrefix(u.Path, "/")
	return
}

// projectFromDSN extracts the database name (project) from the DSN.
func projectFromDSN(dsn string) string {
	_, _, _, _, dbname, err := parseDSN(dsn)
	if err != nil {
		return "backup"
	}
	return dbname
}

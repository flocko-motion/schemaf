// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package schemaftest

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lib/pq"

	"github.com/flocko-motion/schemaf/cli"
	"github.com/flocko-motion/schemaf/db"
)

const (
	postgresImage = "postgres:17-alpine"
	pgUser        = "schemaftest"
	templateDB    = "schemaftest_template"
	// readyTimeout covers an image pull on first use.
	readyTimeout = 3 * time.Minute
)

// holdScript runs Postgres until stdin closes. The test process holds stdin
// through a pipe, so the container stops when the process ends, even on
// SIGKILL; --rm then removes it.
const holdScript = `docker-entrypoint.sh postgres -c fsync=off -c synchronous_commit=off -c full_page_writes=off &
pid=$!
cat >/dev/null
kill -INT "$pid"
wait "$pid"`

// pgContainer is the test binary's one Postgres container.
type pgContainer struct {
	name  string
	pass  string
	port  int
	admin *sql.DB // on the postgres database, for CREATE/DROP DATABASE

	// stdin is the write end of the container's stdin. It is never closed:
	// it closes when the process ends, which stops the container. Keeping
	// it here also keeps the garbage collector from closing it.
	stdin io.WriteCloser
}

var (
	containerMu  sync.Mutex
	container    *pgContainer
	containerErr error
	templateOK   bool
	dbCounter    int
)

// dsn is the address of database name in the container. It is the only
// way the harness builds a DSN: from the container it started.
func (c *pgContainer) dsn(name string) string {
	return fmt.Sprintf("postgres://%s:%s@127.0.0.1:%d/%s?sslmode=disable", pgUser, c.pass, c.port, name)
}

// ensureContainer starts the binary's Postgres container on first use. A
// failure is kept: later instances fail with the same error at once.
func ensureContainer() (*pgContainer, error) {
	containerMu.Lock()
	defer containerMu.Unlock()
	if container == nil && containerErr == nil {
		container, containerErr = startContainer()
	}
	return container, containerErr
}

func startContainer() (*pgContainer, error) {
	if err := cli.CheckEngine(); err != nil {
		return nil, fmt.Errorf("schemaftest needs a container runtime for its Postgres:\n%w", err)
	}
	c := &pgContainer{
		name: "schemaftest-" + strconv.Itoa(os.Getpid()) + "-" + randomHex(4),
		pass: randomHex(16),
	}
	cmd, err := cli.Container("run", "--rm", "-i",
		"--name", c.name,
		"-e", "POSTGRES_USER="+pgUser,
		"-e", "POSTGRES_PASSWORD="+c.pass,
		"-e", "POSTGRES_DB=postgres",
		"-p", "127.0.0.1::5432",
		"--entrypoint", "sh",
		postgresImage, "-c", holdScript)
	if err != nil {
		return nil, err
	}
	out := &tailBuffer{max: 16 << 10}
	cmd.Stdout, cmd.Stderr = out, out
	if c.stdin, err = cmd.StdinPipe(); err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting the Postgres container: %w", err)
	}
	exited := make(chan struct{})
	go func() { cmd.Wait(); close(exited) }()

	if err := c.waitReady(exited); err != nil {
		c.stdin.Close()
		return nil, fmt.Errorf("schemaftest: the Postgres container (%s) did not get ready: %w\ncontainer output:\n%s", postgresImage, err, out.String())
	}
	return c, nil
}

// waitReady waits for the published port, for Postgres to accept TCP
// connections inside the container (the image's init phase listens only on
// the socket), and for a connection from the host.
func (c *pgContainer) waitReady(exited <-chan struct{}) error {
	deadline := time.Now().Add(readyTimeout)
	var last error
	for time.Now().Before(deadline) {
		select {
		case <-exited:
			return errors.New("the container exited (image pull failed, or the runtime refused to start it)")
		default:
		}
		last = c.probe()
		if last == nil {
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("timed out after %s: %v", readyTimeout, last)
}

func (c *pgContainer) probe() error {
	if c.port == 0 {
		cmd, err := cli.Container("port", c.name, "5432/tcp")
		if err != nil {
			return err
		}
		out, err := cmd.Output()
		if err != nil {
			return fmt.Errorf("no published port yet")
		}
		line, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
		_, p, err := net.SplitHostPort(strings.TrimSpace(line))
		if err != nil {
			return fmt.Errorf("parsing the published port %q: %w", line, err)
		}
		if c.port, err = strconv.Atoi(p); err != nil {
			return fmt.Errorf("parsing the published port %q: %w", line, err)
		}
	}
	cmd, err := cli.Container("exec", c.name, "pg_isready", "-h", "127.0.0.1", "-U", pgUser, "-d", "postgres")
	if err != nil {
		return err
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("pg_isready: %s", strings.TrimSpace(string(out)))
	}
	admin, err := sql.Open("postgres", c.dsn("postgres"))
	if err != nil {
		return err
	}
	if err := admin.Ping(); err != nil {
		admin.Close()
		return err
	}
	admin.SetMaxOpenConns(2)
	c.admin = admin
	return nil
}

// ensureTemplate runs the registered migrations (the framework's and the
// app's) once into the template database.
func ensureTemplate(ctx context.Context, c *pgContainer) error {
	containerMu.Lock()
	defer containerMu.Unlock()
	if templateOK {
		return nil
	}
	if _, err := c.admin.ExecContext(ctx, "DROP DATABASE IF EXISTS "+templateDB); err != nil {
		return fmt.Errorf("dropping the old template database: %w", err)
	}
	if _, err := c.admin.ExecContext(ctx, "CREATE DATABASE "+templateDB); err != nil {
		return fmt.Errorf("creating the template database: %w", err)
	}
	tmpl, err := sql.Open("postgres", c.dsn(templateDB))
	if err != nil {
		return err
	}
	defer tmpl.Close()
	if err := db.RunMigrationsOn(ctx, tmpl); err != nil {
		return fmt.Errorf("migrations: %w", err)
	}
	templateOK = true
	return nil
}

// createDatabase creates a new database from the template and returns its
// name. Postgres refuses to copy a template while a connection to it is
// still closing, so that error is retried briefly.
func createDatabase(ctx context.Context, c *pgContainer) (string, error) {
	containerMu.Lock()
	dbCounter++
	name := "schemaftest_" + strconv.Itoa(dbCounter)
	containerMu.Unlock()

	deadline := time.Now().Add(10 * time.Second)
	for {
		_, err := c.admin.ExecContext(ctx, fmt.Sprintf("CREATE DATABASE %s TEMPLATE %s", name, templateDB))
		if err == nil {
			return name, nil
		}
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "55006" && time.Now().Before(deadline) {
			time.Sleep(50 * time.Millisecond)
			continue
		}
		return "", fmt.Errorf("creating database %s from the template: %w", name, err)
	}
}

func dropDatabase(ctx context.Context, c *pgContainer, name string) error {
	if _, err := c.admin.ExecContext(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s WITH (FORCE)", name)); err != nil {
		return fmt.Errorf("dropping database %s: %w", name, err)
	}
	return nil
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// tailBuffer keeps the last max bytes written to it.
type tailBuffer struct {
	mu  sync.Mutex
	max int
	buf bytes.Buffer
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf.Write(p)
	if over := b.buf.Len() - b.max; over > 0 {
		b.buf.Next(over)
	}
	return len(p), nil
}

func (b *tailBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

package schemaf

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestFrontendNeedsInstall(t *testing.T) {
	t0 := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	var missing time.Time

	if _, err := frontendNeedsInstall(missing, t0); err == nil ||
		!strings.Contains(err.Error(), "frontend/package-lock.json is missing — run npm install in frontend/ and commit frontend/package-lock.json") {
		t.Fatalf("lockfile missing: want the refusal, got %v", err)
	}
	for _, c := range []struct {
		name      string
		installed time.Time
		want      bool
	}{
		{"node_modules/.package-lock.json missing", missing, true},
		{"older than the lockfile", t0.Add(-time.Second), true},
		{"same age as the lockfile", t0, false},
		{"newer than the lockfile", t0.Add(time.Second), false},
	} {
		got, err := frontendNeedsInstall(t0, c.installed)
		if err != nil || got != c.want {
			t.Errorf("%s: got %v, %v; want %v", c.name, got, err, c.want)
		}
	}
}

// The fake Vite: the test binary itself, run as `npm` by a script on PATH.
// FAKE_VITE selects what it does.
func TestMain(m *testing.M) {
	switch os.Getenv("FAKE_VITE") {
	case "":
		os.Exit(m.Run())
	case "exit":
		os.Exit(3)
	case "listen", "listen-exit":
		ln, err := net.Listen("tcp", "127.0.0.1:"+os.Getenv("FAKE_VITE_PORT"))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		defer ln.Close()
		go func() {
			for {
				c, err := ln.Accept()
				if err != nil {
					return
				}
				c.Close()
			}
		}()
		if os.Getenv("FAKE_VITE") == "listen-exit" {
			time.Sleep(time.Second)
			os.Exit(2)
		}
		select {}
	}
}

// fakeVite starts a fake `npm run dev` through a script named npm on PATH and
// returns the started command and its port.
func fakeVite(t *testing.T, mode string) (*exec.Cmd, int) {
	t.Helper()
	bin := t.TempDir()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	script := fmt.Sprintf("#!/bin/sh\nexec %q\n", self)
	if err := os.WriteFile(filepath.Join(bin, "npm"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	cmd := exec.Command("npm", "run", "dev")
	cmd.Env = append(os.Environ(), "FAKE_VITE="+mode, "FAKE_VITE_PORT="+strconv.Itoa(port))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	return cmd, port
}

func TestSuperviseVite(t *testing.T) {
	t.Run("exits immediately: failed to start", func(t *testing.T) {
		cmd, port := fakeVite(t, "exit")
		served := false
		err := superviseVite(cmd, port, 10*time.Second, func(<-chan error) error { served = true; return nil })
		if err == nil || err.Error() != "frontend dev server failed to start (exit status 3)" {
			t.Fatalf("want failed to start (exit status 3), got %v", err)
		}
		if served {
			t.Fatal("the backend started although Vite never listened")
		}
	})

	t.Run("never listens: timeout", func(t *testing.T) {
		cmd, _ := fakeVite(t, "listen")
		// The fake listens on another port than the one watched.
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		watched := ln.Addr().(*net.TCPAddr).Port
		ln.Close()
		err = superviseVite(cmd, watched, time.Second, func(<-chan error) error { t.Fatal("served"); return nil })
		if err == nil || !strings.Contains(err.Error(), "frontend dev server failed to start") ||
			!strings.Contains(err.Error(), "not listening on :"+strconv.Itoa(watched)+" after 1s") {
			t.Fatalf("want a timeout, got %v", err)
		}
	})

	t.Run("listens, then exits: dev stops", func(t *testing.T) {
		cmd, port := fakeVite(t, "listen-exit")
		err := superviseVite(cmd, port, 10*time.Second, func(stop <-chan error) error {
			select {
			case err := <-stop:
				return err
			case <-time.After(10 * time.Second):
				return errors.New("the backend was never told to stop")
			}
		})
		if err == nil || err.Error() != "frontend dev server exited (exit status 2) — dev stopped" {
			t.Fatalf("want exited (exit status 2), got %v", err)
		}
	})

	t.Run("listens and stays up: start-up succeeds", func(t *testing.T) {
		cmd, port := fakeVite(t, "listen")
		err := superviseVite(cmd, port, 10*time.Second, func(stop <-chan error) error {
			select {
			case err := <-stop:
				return fmt.Errorf("Vite exited early: %w", err)
			case <-time.After(1500 * time.Millisecond):
				return nil
			}
		})
		if err != nil {
			t.Fatalf("want start-up to succeed, got %v", err)
		}
		// superviseVite stops Vite when the backend returns.
		deadline := time.Now().Add(5 * time.Second)
		for viteListening(port) {
			if time.Now().After(deadline) {
				t.Fatal("Vite still listens after the backend returned")
			}
			time.Sleep(50 * time.Millisecond)
		}
	})
}

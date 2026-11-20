package codegen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// renderScaffold writes the scaffold templates (without npm install) into the
// current directory.
func renderScaffold(t *testing.T) {
	t.Helper()
	if err := os.MkdirAll("frontend/src", 0755); err != nil {
		t.Fatal(err)
	}
	data := map[string]any{"Name": "myapp", "Port": 8000, "FrontendPort": 8002, "PostgresPort": 8003}
	for tmpl, path := range map[string]string{
		scaffoldPackageJSON: "frontend/package.json",
		scaffoldViteConfig:  "frontend/vite.config.ts",
		scaffoldIndexHTML:   "frontend/index.html",
		scaffoldMainTSX:     "frontend/src/main.tsx",
	} {
		if err := renderTemplate(tmpl, path, data); err != nil {
			t.Fatal(err)
		}
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestScaffoldRuntimeConfig(t *testing.T) {
	t.Chdir(t.TempDir())
	renderScaffold(t)

	if s := readFile(t, "frontend/vite.config.ts"); !strings.Contains(s, `target: "es2022"`) {
		t.Errorf("vite.config.ts has no es2022 build target:\n%s", s)
	}
	html := readFile(t, "frontend/index.html")
	if !strings.Contains(html, "This browser is too old to run this app. Please update it.") ||
		!strings.Contains(html, "window.__schemaf_boot") {
		t.Errorf("index.html has no old-browser check:\n%s", html)
	}
	// The check must be a classic script, before the module entry.
	if i, j := strings.Index(html, "<script>"), strings.Index(html, `<script type="module"`); i < 0 || i > j {
		t.Errorf("old-browser check is not an inline classic script before the module:\n%s", html)
	}
	main := readFile(t, "frontend/src/main.tsx")
	firstImport := ""
	for _, line := range strings.Split(main, "\n") {
		if strings.HasPrefix(line, "import ") {
			firstImport = line
			break
		}
	}
	if firstImport != `import "./api/generated/config.gen";` {
		t.Errorf("first import of main.tsx = %q, want the config.gen import", firstImport)
	}

	if err := validateFrontend(); err != nil {
		t.Errorf("fresh scaffold fails validation: %v", err)
	}
}

func TestValidateFrontendRuntimeConfig(t *testing.T) {
	cases := []struct {
		path, old, new, want string
	}{
		{"frontend/vite.config.ts", `target: "es2022",`, "", `target: "es2022"`},
		{"frontend/index.html", "window.__schemaf_boot", "window.x", "old-browser check"},
		{"frontend/src/main.tsx", `import "./api/generated/config.gen";`, "", `import "./api/generated/config.gen";`},
	}
	for _, c := range cases {
		t.Run(c.path, func(t *testing.T) {
			t.Chdir(t.TempDir())
			renderScaffold(t)
			s := readFile(t, c.path)
			if !strings.Contains(s, c.old) {
				t.Fatalf("%s does not contain %q", c.path, c.old)
			}
			if err := os.WriteFile(c.path, []byte(strings.Replace(s, c.old, c.new, 1)), 0644); err != nil {
				t.Fatal(err)
			}
			err := validateFrontend()
			if err == nil {
				t.Fatal("validation passed")
			}
			if !strings.Contains(err.Error(), c.path) || !strings.Contains(err.Error(), c.want) {
				t.Errorf("error does not name %s / %q:\n%v", c.path, c.want, err)
			}
			// Only the broken file is reported.
			for _, other := range cases {
				if other.path != c.path && strings.Contains(err.Error(), "  "+other.path+":") {
					t.Errorf("error also reports %s:\n%v", other.path, err)
				}
			}
		})
	}

	t.Run("newer targets accepted", func(t *testing.T) {
		t.Chdir(t.TempDir())
		renderScaffold(t)
		for _, target := range []string{`'es2023'`, `"esnext"`} {
			s := strings.Replace(readFile(t, "frontend/vite.config.ts"), `"es2022"`, target, 1)
			os.WriteFile("frontend/vite.config.ts", []byte(s), 0644)
			if err := validateFrontend(); err != nil {
				t.Errorf("target %s: %v", target, err)
			}
			renderScaffold(t)
		}
	})
}

// schemaf requires Vite + TypeScript, not React: a non-React entry
// (src/main.ts, as for Vue or Svelte) is accepted, and the runtime config
// check applies to whichever entry file exists.
func TestValidateFrontendEntryFile(t *testing.T) {
	toMainTS := func(t *testing.T) {
		t.Helper()
		if err := os.Rename("frontend/src/main.tsx", "frontend/src/main.ts"); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("main.ts accepted", func(t *testing.T) {
		t.Chdir(t.TempDir())
		renderScaffold(t)
		toMainTS(t)
		if err := validateFrontend(); err != nil {
			t.Errorf("main.ts entry rejected: %v", err)
		}
	})

	t.Run("config import checked in main.ts", func(t *testing.T) {
		t.Chdir(t.TempDir())
		renderScaffold(t)
		toMainTS(t)
		s := strings.Replace(readFile(t, "frontend/src/main.ts"), `import "./api/generated/config.gen";`, "", 1)
		if err := os.WriteFile("frontend/src/main.ts", []byte(s), 0644); err != nil {
			t.Fatal(err)
		}
		err := validateFrontend()
		if err == nil || !strings.Contains(err.Error(), "frontend/src/main.ts:") {
			t.Errorf("missing config import in main.ts not reported for main.ts: %v", err)
		}
	})

	t.Run("no entry file", func(t *testing.T) {
		t.Chdir(t.TempDir())
		renderScaffold(t)
		if err := os.Remove("frontend/src/main.tsx"); err != nil {
			t.Fatal(err)
		}
		err := validateFrontend()
		if err == nil || !strings.Contains(err.Error(), "frontend/src/main.tsx or frontend/src/main.ts") {
			t.Errorf("missing entry not reported with both options: %v", err)
		}
	})

	t.Run("both entry files", func(t *testing.T) {
		t.Chdir(t.TempDir())
		renderScaffold(t)
		if err := os.WriteFile("frontend/src/main.ts", []byte(readFile(t, "frontend/src/main.tsx")), 0644); err != nil {
			t.Fatal(err)
		}
		err := validateFrontend()
		if err == nil || !strings.Contains(err.Error(), "both frontend/src/main.tsx and frontend/src/main.ts exist") {
			t.Errorf("two entry files not refused: %v", err)
		}
	})

	t.Run("error names Vite, not React", func(t *testing.T) {
		t.Chdir(t.TempDir())
		renderScaffold(t)
		if err := os.Remove("frontend/vite.config.ts"); err != nil {
			t.Fatal(err)
		}
		err := validateFrontend()
		if err == nil || !strings.Contains(err.Error(), "Vite + TypeScript") || strings.Contains(err.Error(), "React") {
			t.Errorf("error should name Vite + TypeScript and not React: %v", err)
		}
	})
}

func TestFrontendGenWritesConfigTS(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, d := range []string{"frontend", "go"} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := runFrontendGen(); err != nil {
		t.Fatal(err)
	}
	s := readFile(t, filepath.FromSlash(configGenTSOut))
	for _, want := range []string{
		"DO NOT EDIT",
		`fetch("/api/config"`,
		"const values = await load();",
		"export function config(key: string): string | undefined",
		`alert("Could not load app configuration")`,
		`__schemaf_boot = "ready"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("config.gen.ts lacks %q", want)
		}
	}
	// The boot marker is set before the await, so the old-browser check
	// (which may run on "load" while the fetch is pending) stays quiet.
	if strings.Index(s, `__schemaf_boot = "loading"`) > strings.Index(s, "await load()") {
		t.Error(`__schemaf_boot = "loading" is not set before the top-level await`)
	}
}

func TestComposeBackendLoadsEnvFile(t *testing.T) {
	t.Chdir(t.TempDir())
	writeMinimalProject(t)
	if err := runComposeGen(); err != nil {
		t.Fatal(err)
	}
	s := readFile(t, "compose.gen.yml")
	backend := s[strings.Index(s, "  backend:"):]
	if !strings.Contains(backend, "env_file:\n      - path: ${HOME}/.myapp/etc/env") {
		t.Errorf("backend service does not load ~/.myapp/etc/env:\n%s", backend)
	}
	if strings.Contains(s, "AUTH0") {
		t.Errorf("compose.gen.yml still passes AUTH0 variables explicitly:\n%s", s)
	}
}

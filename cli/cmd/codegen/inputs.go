// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package codegen

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	cli "github.com/flocko-motion/schemaf/cli"
)

// inputsHashFile records the hash of codegen's normative inputs. build and
// test (schemaf.sh) recompute it and refuse when it doesn't match, i.e. when
// generated files are stale. See EXTEND.md › Code Generation.
const inputsHashFile = "gen/inputs.sha256"

// schemafModule is the framework's module path, as required in go/go.mod.
const schemafModule = "github.com/flocko-motion/schemaf"

// inputGlobs are codegen's normative inputs, relative to the project root.
// This is the single definition: schemaf.sh.tmpl renders the same list into
// its shell implementation (_schemaf_inputs_hash), and a test checks that
// both compute the same hash. Globs match like bash without dotglob: names
// starting with "." are never inputs.
var inputGlobs = []string{
	"go/api/*.go",
	"go/db/migrations/*",
	"go/db/queries/*",
	"schemaf.toml",
	"compose/*.yml",
}

// inputExcludes are globs removed from the matches of inputGlobs.
var inputExcludes = []string{"go/api/*.gen.go"}

// inputFiles returns the regular files matched by inputGlobs, minus
// inputExcludes and dotfiles, sorted byte-wise.
func inputFiles() ([]string, error) {
	var files []string
	for _, g := range inputGlobs {
		matches, err := filepath.Glob(g)
		if err != nil {
			return nil, fmt.Errorf("input glob %s: %w", g, err)
		}
	next:
		for _, m := range matches {
			if strings.HasPrefix(filepath.Base(m), ".") {
				continue
			}
			for _, ex := range inputExcludes {
				if ok, _ := filepath.Match(ex, m); ok {
					continue next
				}
			}
			info, err := os.Stat(m)
			if err != nil {
				return nil, err
			}
			if info.Mode().IsRegular() {
				files = append(files, filepath.ToSlash(m))
			}
		}
	}
	sort.Strings(files)
	return files, nil
}

// inputsHash computes the hash of codegen's inputs in the current directory
// (the project root): for each input file in sorted order the line
// "<sha256 of file>  <path>\n" (sha256sum's format), then "schemaf <version>\n";
// the result is the hex SHA-256 of that text.
func inputsHash() (string, error) {
	files, err := inputFiles()
	if err != nil {
		return "", err
	}
	version, err := readSchemafVersion()
	if err != nil {
		return "", err
	}
	var manifest strings.Builder
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return "", err
		}
		sum := sha256.Sum256(data)
		fmt.Fprintf(&manifest, "%s  %s\n", hex.EncodeToString(sum[:]), f)
	}
	fmt.Fprintf(&manifest, "schemaf %s\n", version)
	sum := sha256.Sum256([]byte(manifest.String()))
	return hex.EncodeToString(sum[:]), nil
}

// runInputsHashGen writes gen/inputs.sha256. It is the last codegen step.
func runInputsHashGen() error {
	h, err := inputsHash()
	if err != nil {
		return err
	}
	if err := os.MkdirAll("gen", 0755); err != nil {
		return fmt.Errorf("creating gen/: %w", err)
	}
	if err := os.WriteFile(inputsHashFile, []byte(h+"\n"), 0644); err != nil {
		return fmt.Errorf("writing %s: %w", inputsHashFile, err)
	}
	cli.Success("Generated %s", inputsHashFile)
	return nil
}

// readSchemafVersion returns the schemaf version required in go/go.mod. The
// rule matches the shell's (_schemaf_version in schemaf.sh.tmpl): the first
// non-comment line holding the module path directly followed by a field
// starting with "v". A go.mod without one is an error.
func readSchemafVersion() (string, error) {
	f, err := os.Open("go/go.mod")
	if err != nil {
		return "", fmt.Errorf("reading the schemaf version: %w", err)
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) > 0 && strings.HasPrefix(fields[0], "//") {
			continue
		}
		for i := 0; i+1 < len(fields); i++ {
			if fields[i] == schemafModule && strings.HasPrefix(fields[i+1], "v") {
				return fields[i+1], nil
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("go/go.mod requires no version of %s — run: cd go && go get %s@latest", schemafModule, schemafModule)
}

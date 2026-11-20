// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package files

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/flocko-motion/schemaf/constants"
)

// The checkout's dev instance sticks in <project root>/.schemaf/instance
// (INSTALL.md › Parallel dev instances). .schemaf/ carries its own .gitignore,
// so projects don't edit theirs.
const (
	instanceDirName  = ".schemaf"
	instanceFileName = "instance"
)

func instanceFilePath(root string) string {
	return filepath.Join(root, instanceDirName, instanceFileName)
}

// readInstanceFile returns the checkout's sticky instance, or ok=false when
// the checkout has none. A file that doesn't hold an instance number is an error.
func readInstanceFile(root string) (n int, ok bool, err error) {
	b, err := os.ReadFile(instanceFilePath(root))
	if errors.Is(err, os.ErrNotExist) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("reading %s/%s: %w", instanceDirName, instanceFileName, err)
	}
	s := strings.TrimSpace(string(b))
	n, err = constants.ParseInstance(s)
	if err != nil || n == 0 {
		return 0, false, fmt.Errorf("%s/%s in %s holds %q, not an instance number — fix it, or switch back with ./schemaf.sh dev --instance default",
			instanceDirName, instanceFileName, root, s)
	}
	return n, true, nil
}

// ResolveInstance returns the dev instance (0 = default) a command in the
// checkout at root uses. flag is the --instance value ("" when not given).
//
//   - Without a flag: the instance in .schemaf/instance, else the default.
//   - With a flag in a checkout without the file: the flag.
//   - With a flag that differs from the file: an error. Only dev (isDev) may
//     pass "default" there, which switches the checkout back.
//
// It never writes the file; dev does that with PersistInstance.
func ResolveInstance(root, flag string, isDev bool) (int, error) {
	sticky, hasSticky, err := readInstanceFile(root)
	if err != nil {
		return 0, err
	}
	if flag == "" {
		return sticky, nil
	}
	n, err := constants.ParseInstance(flag)
	if err != nil {
		return 0, err
	}
	if !hasSticky || n == sticky || (isDev && n == 0) {
		return n, nil
	}
	return 0, fmt.Errorf("this checkout is instance %d (%s/%s), but --instance %s was given — drop the flag, or switch the checkout back first with ./schemaf.sh dev --instance default",
		sticky, instanceDirName, instanceFileName, flag)
}

// PersistInstance records dev's --instance flag in the checkout at root:
// a number is written to .schemaf/instance, "default" removes the file, and
// no flag ("") leaves it as it is. Call it after ResolveInstance accepted flag.
func PersistInstance(root, flag string) error {
	if flag == "" {
		return nil
	}
	n, err := constants.ParseInstance(flag)
	if err != nil {
		return err
	}
	if n == 0 {
		if err := os.Remove(instanceFilePath(root)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("removing %s/%s: %w", instanceDirName, instanceFileName, err)
		}
		return nil
	}
	dir := filepath.Join(root, instanceDirName)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating %s/: %w", instanceDirName, err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("*\n"), 0644); err != nil {
		return fmt.Errorf("writing %s/.gitignore: %w", instanceDirName, err)
	}
	if err := os.WriteFile(instanceFilePath(root), []byte(constants.InstanceName(n)+"\n"), 0644); err != nil {
		return fmt.Errorf("writing %s/%s: %w", instanceDirName, instanceFileName, err)
	}
	return nil
}

// InstanceRecord is what dev records about a parallel dev instance in
// ~/.<name>/dev/instances/<n>/instance.toml: its ports and its checkout.
// Whether it runs is asked from the container runtime, never recorded.
type InstanceRecord struct {
	Instance     int    `toml:"-"`
	BackendPort  int    `toml:"backend_port"`
	VitePort     int    `toml:"vite_port"`
	PostgresPort int    `toml:"postgres_port"`
	Checkout     string `toml:"checkout"` // absolute path of the checkout
}

// InstancesDir returns ~/.<name>/dev/instances, which holds one directory per
// parallel dev instance.
func InstancesDir() string { return filepath.Join(ProjectHome(), "dev", "instances") }

// InstanceDir returns ~/.<name>/dev/instances/<n>: instance n's record and data.
func InstanceDir(n int) string { return filepath.Join(InstancesDir(), strconv.Itoa(n)) }

func instanceRecordPath(n int) string { return filepath.Join(InstanceDir(n), "instance.toml") }

// WriteInstanceRecord writes r to its instance's instance.toml.
func WriteInstanceRecord(r InstanceRecord) error {
	if r.Instance < 1 {
		return fmt.Errorf("instance record needs an instance number ≥ 1, got %d", r.Instance)
	}
	b, err := toml.Marshal(r)
	if err != nil {
		return fmt.Errorf("encoding instance %d record: %w", r.Instance, err)
	}
	if err := os.MkdirAll(InstanceDir(r.Instance), 0755); err != nil {
		return fmt.Errorf("creating %s: %w", InstanceDir(r.Instance), err)
	}
	header := "# Written by ./schemaf.sh dev --instance (INSTALL.md › Parallel dev instances).\n"
	if err := os.WriteFile(instanceRecordPath(r.Instance), append([]byte(header), b...), 0644); err != nil {
		return fmt.Errorf("writing %s: %w", instanceRecordPath(r.Instance), err)
	}
	return nil
}

// ReadInstanceRecord reads instance n's record; ok is false when there is none.
func ReadInstanceRecord(n int) (r InstanceRecord, ok bool, err error) {
	b, err := os.ReadFile(instanceRecordPath(n))
	if errors.Is(err, os.ErrNotExist) {
		return InstanceRecord{}, false, nil
	}
	if err != nil {
		return InstanceRecord{}, false, fmt.Errorf("reading %s: %w", instanceRecordPath(n), err)
	}
	if err := toml.Unmarshal(b, &r); err != nil {
		return InstanceRecord{}, false, fmt.Errorf("parsing %s: %w", instanceRecordPath(n), err)
	}
	r.Instance = n
	return r, true, nil
}

// InstanceRecords returns the records of every parallel dev instance on this
// machine, ordered by number. Directories that aren't instances are ignored.
func InstanceRecords() ([]InstanceRecord, error) {
	entries, err := os.ReadDir(InstancesDir())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", InstancesDir(), err)
	}
	var recs []InstanceRecord
	for _, e := range entries {
		n, err := constants.ParseInstance(e.Name())
		if !e.IsDir() || err != nil || n == 0 {
			continue
		}
		r, ok, err := ReadInstanceRecord(n)
		if err != nil {
			return nil, err
		}
		if ok {
			recs = append(recs, r)
		}
	}
	sort.Slice(recs, func(i, j int) bool { return recs[i].Instance < recs[j].Instance })
	return recs, nil
}

// ClaimInstance checks that instance n may run from the checkout at root.
// An instance belongs to one checkout: if its record names another checkout
// that still claims n in its .schemaf/instance, that is a conflict. If the
// recorded checkout is gone or no longer claims n (moved, removed, switched
// back), root takes the instance over, and took names the previous checkout.
// The caller rewrites the record (WriteInstanceRecord).
func ClaimInstance(n int, root string) (took string, err error) {
	rec, ok, err := ReadInstanceRecord(n)
	if err != nil || !ok || rec.Checkout == root {
		return "", err
	}
	claimed, has, err := readInstanceFile(rec.Checkout)
	if err != nil {
		return "", err
	}
	if has && claimed == n {
		return "", fmt.Errorf("instance %d belongs to the checkout %s — pick another number, or remove it first: ./schemaf.sh instances rm %d",
			n, rec.Checkout, n)
	}
	return rec.Checkout, nil
}

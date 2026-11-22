// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// The run host's ~/.<name>/updates/, as the backend container sees it:
// available.json, status.json and the marker updater read-only, request
// writable (INSTALL.md › The updater). A variable for tests.
var updatesDir = "/schemaf/updates"

// Errors of RequestUpgrade; compare with errors.Is. See EXTEND.md, "Updates".
var (
	ErrUpdaterNotEnabled = Error(ErrConflict, "updater_not_enabled",
		"updates: the updater is not enabled on this host (run host: schemaf.sh updater enable)")
	ErrVersionNotAvailable = Error(ErrUnprocessable, "version_not_available",
		"updates: this version is not available — request one of the versions api.Updates() lists")
	ErrInstallRunning = Error(ErrConflict, "install_running",
		"updates: an install is running — request again when it has finished")
)

// UpdateState is what api.Updates returns and GET /api/updates answers.
type UpdateState struct {
	Running   string             `json:"running"`   // the running version; empty in development
	Status    string             `json:"status"`    // "updater not enabled", "idle" or "installing"
	Available []AvailableRelease `json:"available"` // verified versions newer than Running, oldest first
	Checked   string             `json:"checked,omitempty"`
	Install   *InstallState      `json:"install,omitempty"` // the last or current install
	Request   *RequestState      `json:"request,omitempty"` // the last request the updater handled
}

// AvailableRelease is a verified version the app may request.
type AvailableRelease struct {
	Version string `json:"version"`
	Notes   string `json:"notes"`
}

// InstallState is the last or current install on the run host.
type InstallState struct {
	Version  string `json:"version"`
	From     string `json:"from"`
	State    string `json:"state"` // installing, installed, rolled-back, failed, rollback-failed
	Started  string `json:"started"`
	Finished string `json:"finished"`
	Message  string `json:"message"`
}

// RequestState is the last request the updater handled.
type RequestState struct {
	Version string `json:"version"`
	State   string `json:"state"` // accepted, rejected
	Message string `json:"message"`
	Time    string `json:"time"`
}

// Update statuses.
const (
	UpdatesNotEnabled = "updater not enabled"
	UpdatesIdle       = "idle"
	UpdatesInstalling = "installing"
)

// runningVersion is the tag of the backend's image (SCHEMAF_IMAGE, set by
// schemaf.sh and passed into the container by compose.gen.yml) if it is a
// version; empty otherwise (development, a commit tag).
func runningVersion() string {
	img := os.Getenv("SCHEMAF_IMAGE")
	tag := img[strings.LastIndex(img, ":")+1:]
	if !versionPattern.MatchString(tag) {
		return ""
	}
	return tag
}

// newerVersion reports whether version a is newer than b by semver
// precedence. Both match versionPattern.
func newerVersion(a, b string) bool {
	split := func(v string) (nums []string, pre string) {
		core, pre, _ := strings.Cut(v, "-")
		return strings.Split(core, "."), pre
	}
	// cmpNum compares decimal strings without leading zeros.
	cmpNum := func(x, y string) int {
		if len(x) != len(y) {
			if len(x) < len(y) {
				return -1
			}
			return 1
		}
		return strings.Compare(x, y)
	}
	an, ap := split(a)
	bn, bp := split(b)
	for i := 0; i < 3; i++ {
		if c := cmpNum(an[i], bn[i]); c != 0 {
			return c > 0
		}
	}
	switch {
	case ap == bp:
		return false
	case ap == "":
		return true // a release is newer than its prereleases
	case bp == "":
		return false
	}
	isNum := numericIdentifier.MatchString
	ai, bi := strings.Split(ap, "."), strings.Split(bp, ".")
	for i := 0; i < len(ai) && i < len(bi); i++ {
		x, y := ai[i], bi[i]
		if x == y {
			continue
		}
		switch {
		case isNum(x) && isNum(y):
			return cmpNum(x, y) > 0
		case isNum(x):
			return false // numeric identifiers are older than alphanumeric ones
		case isNum(y):
			return true
		default:
			return x > y
		}
	}
	return len(ai) > len(bi)
}

// updaterEnabled reports whether the run host's updater is enabled: it
// writes the marker updater, and the run side mounts request.
func updaterEnabled() bool {
	_, errMarker := os.Stat(filepath.Join(updatesDir, "updater"))
	_, errRequest := os.Stat(filepath.Join(updatesDir, "request"))
	return errMarker == nil && errRequest == nil
}

// readUpdatesFile decodes updatesDir/name into v; a missing file is no error.
func readUpdatesFile(name string, v any) error {
	b, err := os.ReadFile(filepath.Join(updatesDir, name))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("updates: reading %s: %w", name, err)
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		return nil
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("updates: %s is not valid JSON: %w", name, err)
	}
	return nil
}

// Updates returns the running version, the versions the app may request and
// the state of the last install and request. Without an updater it returns
// the running version, an empty list and the status UpdatesNotEnabled. See
// EXTEND.md, "Updates".
func Updates() (UpdateState, error) {
	st := UpdateState{Running: runningVersion(), Status: UpdatesNotEnabled, Available: []AvailableRelease{}}
	if !updaterEnabled() {
		return st, nil
	}
	var available struct {
		Checked  string             `json:"checked"`
		Versions []AvailableRelease `json:"versions"`
	}
	if err := readUpdatesFile("available.json", &available); err != nil {
		return st, err
	}
	var status struct {
		Running string        `json:"running"`
		Install *InstallState `json:"install"`
		Request *RequestState `json:"request"`
	}
	if err := readUpdatesFile("status.json", &status); err != nil {
		return st, err
	}
	st.Status = UpdatesIdle
	// available.json is refreshed by the next releases run; until then it may
	// still list what is now running, or older.
	for _, a := range available.Versions {
		if !versionPattern.MatchString(a.Version) || (st.Running != "" && !newerVersion(a.Version, st.Running)) {
			continue
		}
		st.Available = append(st.Available, a)
	}
	st.Checked = available.Checked
	if status.Install != nil && status.Install.Version != "" {
		st.Install = status.Install
		if st.Install.State == "installing" {
			st.Status = UpdatesInstalling
		}
	}
	if status.Request != nil && status.Request.Version != "" {
		st.Request = status.Request
	}
	return st, nil
}

var numericIdentifier = regexp.MustCompile(`^[0-9]+$`)

var versionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$`)

// RequestUpgrade asks the run host's updater to install version: it writes
// the version to the request file. The updater checks it again and decides.
// Errors: ErrUpdaterNotEnabled, ErrVersionNotAvailable, ErrInstallRunning.
func RequestUpgrade(version string) error {
	if !updaterEnabled() {
		return ErrUpdaterNotEnabled
	}
	st, err := Updates()
	if err != nil {
		return err
	}
	if st.Status == UpdatesInstalling {
		return ErrInstallRunning
	}
	if !versionPattern.MatchString(version) {
		return ErrVersionNotAvailable
	}
	found := false
	for _, a := range st.Available {
		found = found || a.Version == version
	}
	if !found {
		return ErrVersionNotAvailable
	}
	// The request file is a bind mount: write it in place, never replace it.
	f, err := os.OpenFile(filepath.Join(updatesDir, "request"), os.O_WRONLY|os.O_TRUNC, 0)
	if err != nil {
		return fmt.Errorf("updates: writing the request: %w", err)
	}
	if _, err := io.WriteString(f, version+"\n"); err != nil {
		f.Close()
		return fmt.Errorf("updates: writing the request: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("updates: writing the request: %w", err)
	}
	return nil
}

// UpdatesAuthFunc decides whether the caller may use the update endpoints.
type UpdatesAuthFunc func(ctx context.Context) error

var updatesAuth UpdatesAuthFunc

// SetUpdatesAuth sets the authorizer of GET /api/updates and POST
// /api/updates/request. nil means nobody may. Called by the schemaf app at
// server start (see app.SetUpdatesAuth).
func SetUpdatesAuth(fn UpdatesAuthFunc) {
	updatesAuth = fn
}

const maxUpdatesRequestBody = 1 << 10

// updatesAuthorized answers the request itself and returns false if the
// caller may not use the update endpoints.
func updatesAuthorized(w http.ResponseWriter, r *http.Request) bool {
	if updatesAuth == nil {
		WriteError(w, r, Error(ErrForbidden, "forbidden", "updates: the app has not authorized anyone (app.SetUpdatesAuth)"))
		return false
	}
	if err := updatesAuth(r.Context()); err != nil {
		WriteError(w, r, err)
		return false
	}
	return true
}

// updatesHandler serves GET /api/updates.
func updatesHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !updatesAuthorized(w, r) {
			return
		}
		st, err := Updates()
		if err != nil {
			WriteError(w, r, err)
			return
		}
		writeJSON(w, st)
	})
}

// updatesRequestHandler serves POST /api/updates/request.
func updatesRequestHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !updatesAuthorized(w, r) {
			return
		}
		var body struct {
			Version string `json:"version"`
		}
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxUpdatesRequestBody))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&body); err != nil || body.Version == "" {
			WriteError(w, r, Error(ErrBadRequest, "bad_request", `updates: the body must be {"version": "<version>"}`))
			return
		}
		if err := RequestUpgrade(body.Version); err != nil {
			WriteError(w, r, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(map[string]string{"version": body.Version})
	})
}

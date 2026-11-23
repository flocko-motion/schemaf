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
	"time"
)

// The run host's ~/.<name>/updates/, as the backend container sees it:
// available.json, status.json and the marker updater read-only, request
// writable (INSTALL.md › The updater). A variable for tests.
var updatesDir = "/schemaf/updates"

// Errors of RequestUpgrade and CheckForUpdates; compare with errors.Is. See
// EXTEND.md, "Updates".
var (
	ErrUpdaterNotEnabled = Error(ErrConflict, "updater_not_enabled",
		"updates: the updater is not enabled on this host (run host: schemaf.sh updater enable)")
	ErrVersionNotAvailable = Error(ErrUnprocessable, "version_not_available",
		"updates: this version is not available — request one of the versions api.Updates() lists")
	ErrInstallRunning = Error(ErrConflict, "install_running",
		"updates: an install is running — request again when it has finished")
	ErrCheckRunning = Error(ErrConflict, "check_running",
		"updates: a check for new releases is running — see api.Updates() for its outcome")
	// ErrCheckTooSoon: CheckForUpdates returns an error that matches it
	// (errors.Is) and says how long to wait.
	ErrCheckTooSoon = Error(ErrTooManyRequests, "check_too_soon",
		"updates: the last check finished less than a minute ago")
)

// checkInterval is the least time between two checks, the run side's limit
// too (INSTALL.md › The updater). A check that started more than checkStale
// ago and is still recorded as checking was interrupted.
const (
	checkInterval = time.Minute
	checkStale    = 30 * time.Minute
)

// UpdateState is what api.Updates returns and GET /api/updates answers.
type UpdateState struct {
	Running   string             `json:"running"`   // the running version; empty in development
	Status    string             `json:"status"`    // "updater not enabled", "idle" or "installing"
	Available []AvailableRelease `json:"available"` // verified versions newer than Running, oldest first
	Checked   string             `json:"checked,omitempty"`
	Install   *InstallState      `json:"install,omitempty"` // the last or current install
	Request   *RequestState      `json:"request,omitempty"` // the last install request the updater handled
	Check     *CheckState        `json:"check,omitempty"`   // the last or current check for new releases
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

// CheckState is the last or current check for new releases (a releases run)
// on the run host.
type CheckState struct {
	State    string `json:"state"`   // checking, checked, failed, rejected
	Trigger  string `json:"trigger"` // timer, app, operator
	Started  string `json:"started"`
	Finished string `json:"finished"`
	Message  string `json:"message"`
}

// Update statuses.
const (
	UpdatesNotEnabled = "updater not enabled"
	UpdatesIdle       = "idle"
	UpdatesInstalling = "installing"
	UpdatesChecking   = "checking"
)

// runningVersion is the running release: RunningVersion if it is a version;
// empty otherwise (development, a commit tag).
func runningVersion() string {
	if v := RunningVersion(); versionPattern.MatchString(v) {
		return v
	}
	return ""
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
		Check   *CheckState   `json:"check"`
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
	if status.Check != nil && status.Check.State != "" {
		st.Check = status.Check
		if st.Status == UpdatesIdle && checkRunning(st.Check) {
			st.Status = UpdatesChecking
		}
	}
	return st, nil
}

// checkRunning reports whether c is a check that runs: recorded as checking,
// started less than checkStale ago.
func checkRunning(c *CheckState) bool {
	if c.State != "checking" {
		return false
	}
	started, err := time.Parse(time.RFC3339, c.Started)
	return err == nil && now().Sub(started) < checkStale
}

// lastCheckFinished is when the last check that ran finished: a finished or
// failed check record (an interrupted one has no Finished), or the last
// refresh of available.json, whichever is later. Zero if neither is known.
func lastCheckFinished(st UpdateState) time.Time {
	var last time.Time
	if c := st.Check; c != nil && (c.State == "checked" || c.State == "failed") {
		if t, err := time.Parse(time.RFC3339, c.Finished); err == nil {
			last = t
		}
	}
	if t, err := time.Parse(time.RFC3339, st.Checked); err == nil && t.After(last) {
		last = t
	}
	return last
}

// checkTooSoonError matches ErrCheckTooSoon and says how long to wait.
type checkTooSoonError struct{ err error }

func (e checkTooSoonError) Error() string        { return e.err.Error() }
func (e checkTooSoonError) Is(target error) bool { return target == ErrCheckTooSoon }
func (e checkTooSoonError) Unwrap() error        { return e.err }

// ceilSeconds is d in whole seconds, rounded up.
func ceilSeconds(d time.Duration) int64 {
	return int64((d + time.Second - 1) / time.Second)
}

// CheckForUpdates asks the run host's updater to look for new releases now:
// it writes the word check to the request file. The updater runs releases
// (fetch and verify, never install) and enforces the same limits on its side.
// Poll Updates for the outcome. Errors: ErrUpdaterNotEnabled,
// ErrInstallRunning, ErrCheckRunning, ErrCheckTooSoon. See EXTEND.md,
// "Updates".
func CheckForUpdates() error {
	if !updaterEnabled() {
		return ErrUpdaterNotEnabled
	}
	st, err := Updates()
	if err != nil {
		return err
	}
	switch st.Status {
	case UpdatesInstalling:
		return ErrInstallRunning
	case UpdatesChecking:
		return ErrCheckRunning
	}
	if last := lastCheckFinished(st); !last.IsZero() {
		since := now().Sub(last)
		if wait := checkInterval - since; wait > 0 {
			if since < 0 {
				since, wait = 0, checkInterval // a clock ahead of ours
			}
			msg := fmt.Sprintf("updates: the last check finished %ds ago — check again in %ds", int64(since/time.Second), ceilSeconds(wait))
			return RetryAfter(checkTooSoonError{Error(ErrTooManyRequests, "check_too_soon", msg)}, wait)
		}
	}
	return writeRequest("check")
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
	return writeRequest(version)
}

// writeRequest writes line to the updater's request file.
func writeRequest(line string) error {
	// The request file is a bind mount: write it in place, never replace it.
	f, err := os.OpenFile(filepath.Join(updatesDir, "request"), os.O_WRONLY|os.O_TRUNC, 0)
	if err != nil {
		return fmt.Errorf("updates: writing the request: %w", err)
	}
	if _, err := io.WriteString(f, line+"\n"); err != nil {
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

// SetUpdatesAuth sets the authorizer of GET /api/updates, POST
// /api/updates/request and POST /api/updates/check. nil means nobody may. Called by the schemaf app at
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

// updatesCheckHandler serves POST /api/updates/check. It has no body.
func updatesCheckHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !updatesAuthorized(w, r) {
			return
		}
		if err := CheckForUpdates(); err != nil {
			WriteError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	})
}

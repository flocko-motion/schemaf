// Package scripts holds no Go code: these tests run scripts/merge.sh and
// scripts/release.sh in a throwaway git repository with a local bare "origin",
// and with stub `gh`, `go`, `sleep` and `uname` binaries first on PATH, so
// nothing reaches GitHub and nothing waits.
package scripts

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ghStub fakes the gh calls of merge.sh and release.sh. It logs every call to
// $GH_LOG and keeps state in $GH_STATE. $GH_SCENARIO picks the CI outcome:
//
//	green    the run is queued, then in progress, then completed/success
//	red      the run completes with failure
//	timeout  the run stays in progress forever
//	notfound no run ever shows up
//	several  two runs for the same commit
const ghStub = `#!/usr/bin/env bash
echo "gh $*" >> "$GH_LOG"
case "$1 $2" in
  "pr view")
    if [[ "$*" == *"--json state"* ]]; then
      [ -f "$GH_STATE/pr" ] && echo OPEN && exit 0
      exit 1
    fi
    echo "" ;;
  "pr create") touch "$GH_STATE/pr" ;;
  "pr merge")
    # Like GitHub: a merge commit of origin/main and the PR head on origin's
    # main. GH_MAIN_MOVED=1: main got another commit first, so the merge
    # commit's tree is not the branch's.
    touch "$GH_STATE/merged"
    base=$(git rev-parse origin/main)
    if [ "${GH_MAIN_MOVED:-}" = 1 ]; then
      blob=$(echo moved | git hash-object -w --stdin)
      tree=$(printf '100644 blob %s\tmoved.txt\n' "$blob" | git mktree)
      base=$(git commit-tree "$tree" -p "$base" -m "someone else")
      tree=$( (git ls-tree HEAD; printf '100644 blob %s\tmoved.txt\n' "$blob") | git mktree)
    else
      tree=$(git rev-parse 'HEAD^{tree}')
    fi
    merge=$(git commit-tree "$tree" -p "$base" -p HEAD -m "Merge pull request")
    git push -q origin "$merge:refs/heads/main" ;;
  "run list")
    case "$GH_SCENARIO" in
      notfound) ;;
      several) echo "1 https://github.example/runs/1"; echo "2 https://github.example/runs/2" ;;
      *) echo "4242 https://github.example/runs/4242" ;;
    esac ;;
  "run view")
    n=$(cat "$GH_STATE/views" 2>/dev/null || echo 0); n=$((n + 1)); echo "$n" > "$GH_STATE/views"
    case "$GH_SCENARIO" in
      green) if [ "$n" -eq 1 ]; then echo "queued "; elif [ "$n" -eq 2 ]; then echo "in_progress "; else echo "completed success"; fi ;;
      red) echo "completed failure" ;;
      timeout) echo "in_progress " ;;
    esac ;;
  "release create") touch "$GH_STATE/release" ;;
  *) echo "unexpected gh call: gh $*" >&2; exit 99 ;;
esac
`

type sandbox struct {
	t      *testing.T
	root   string // the temp dir
	work   string // the clone the scripts run in
	origin string // the bare "origin"
	env    []string
}

func newSandbox(t *testing.T) *sandbox {
	t.Helper()
	for _, tool := range []string{"bash", "git"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Fatalf("the script tests need %s on PATH", tool)
		}
	}
	root := t.TempDir()
	s := &sandbox{t: t, root: root, work: filepath.Join(root, "work"), origin: filepath.Join(root, "origin.git")}

	bin := filepath.Join(root, "bin")
	state := filepath.Join(root, "ghstate")
	for _, d := range []string{bin, state} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	stubs := map[string]string{
		"gh":    ghStub,
		"go":    "#!/usr/bin/env bash\necho \"go $*\" >> \"$GO_LOG\"\n",
		"sleep": "#!/usr/bin/env bash\nexit 0\n",
		"uname": "#!/usr/bin/env bash\necho \"${FAKE_UNAME:-Linux}\"\n",
	}
	for name, body := range stubs {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	s.env = append(os.Environ(),
		"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"HOME="+root,
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.invalid",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.invalid",
		"GH_LOG="+filepath.Join(root, "gh.log"),
		"GH_STATE="+state,
		"GO_LOG="+filepath.Join(root, "go.log"),
		"SCHEMAF_RUNTIME=",
	)

	s.git(root, "init", "-q", "--bare", "-b", "main", s.origin)
	s.git(root, "init", "-q", "-b", "main", s.work)
	for _, f := range []string{"merge.sh", "release.sh", "issues.sh", "test.sh"} {
		s.copy(f, filepath.Join("scripts", f))
	}
	s.write(".gitignore", "")
	s.git(s.work, "add", "-A")
	s.git(s.work, "commit", "-q", "-m", "init")
	s.git(s.work, "remote", "add", "origin", s.origin)
	s.git(s.work, "push", "-q", "-u", "origin", "main")
	return s
}

func (s *sandbox) copy(name, dst string) {
	s.t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		s.t.Fatal(err)
	}
	p := filepath.Join(s.work, dst)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		s.t.Fatal(err)
	}
	if err := os.WriteFile(p, b, 0o755); err != nil {
		s.t.Fatal(err)
	}
}

func (s *sandbox) write(name, body string) {
	s.t.Helper()
	p := filepath.Join(s.work, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		s.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
		s.t.Fatal(err)
	}
}

func (s *sandbox) git(dir string, args ...string) string {
	s.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = s.env
	out, err := cmd.CombinedOutput()
	if err != nil {
		s.t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// run runs a script in the clone with extra env vars; it returns the combined
// output and whether it exited 0.
func (s *sandbox) run(script string, extraEnv []string, args ...string) (string, bool) {
	s.t.Helper()
	cmd := exec.Command("bash", append([]string{script}, args...)...)
	cmd.Dir = s.work
	cmd.Env = append(append([]string{}, s.env...), extraEnv...)
	out, err := cmd.CombinedOutput()
	return string(out), err == nil
}

func (s *sandbox) exists(name string) bool {
	_, err := os.Stat(filepath.Join(s.root, name))
	return err == nil
}

func (s *sandbox) ghLog() string {
	b, _ := os.ReadFile(filepath.Join(s.root, "gh.log"))
	return string(b)
}

// featureBranch makes a branch with one commit beyond main and returns its head.
func (s *sandbox) featureBranch() string {
	s.git(s.work, "switch", "-q", "-c", "feat")
	s.write("feature.txt", "x\n")
	s.git(s.work, "add", "-A")
	s.git(s.work, "commit", "-q", "-m", "feat: a feature")
	return s.git(s.work, "rev-parse", "HEAD")
}

func TestMergeWaitsForGreenGate(t *testing.T) {
	s := newSandbox(t)
	sha := s.featureBranch()
	out, ok := s.run("scripts/merge.sh", []string{"GH_SCENARIO=green"})
	if !ok {
		t.Fatalf("merge.sh failed on a green gate:\n%s", out)
	}
	if !s.exists("ghstate/merged") {
		t.Fatalf("green gate, but the PR was not merged:\n%s", out)
	}
	want := fmt.Sprintf("gh pr merge feat --merge --match-head-commit %s --delete-branch=false", sha)
	if !strings.Contains(s.ghLog(), want) {
		t.Errorf("gh log lacks %q:\n%s", want, s.ghLog())
	}
	if !strings.Contains(s.ghLog(), "gh run list --workflow gate.yml --commit "+sha+" --event pull_request") {
		t.Errorf("the run was not looked up by workflow + head SHA + event:\n%s", s.ghLog())
	}
	if !strings.Contains(out, "https://github.example/runs/4242") {
		t.Errorf("output lacks the run URL:\n%s", out)
	}
}

func TestMergeRedGateMergesNothing(t *testing.T) {
	s := newSandbox(t)
	s.featureBranch()
	out, ok := s.run("scripts/merge.sh", []string{"GH_SCENARIO=red"})
	if ok {
		t.Fatalf("merge.sh succeeded on a red gate:\n%s", out)
	}
	if s.exists("ghstate/merged") {
		t.Fatalf("red gate, but the PR was merged:\n%s", out)
	}
	for _, want := range []string{"CI gate is red (failure)", "nothing merged", "https://github.example/runs/4242"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestMergeTimeoutIsAFailure(t *testing.T) {
	s := newSandbox(t)
	s.featureBranch()
	out, ok := s.run("scripts/merge.sh", []string{"GH_SCENARIO=timeout"})
	if ok {
		t.Fatalf("merge.sh succeeded although the gate never finished:\n%s", out)
	}
	if s.exists("ghstate/merged") {
		t.Fatalf("timeout, but the PR was merged:\n%s", out)
	}
	for _, want := range []string{"didn't finish within 45 minutes", "https://github.example/runs/4242"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	// 45 minutes of 20 s polls.
	if n := strings.Count(s.ghLog(), "gh run view"); n != 45*60/20+1 {
		t.Errorf("polled the run %d times, want %d", n, 45*60/20+1)
	}
}

func TestMergeRunNotFoundIsLoud(t *testing.T) {
	s := newSandbox(t)
	sha := s.featureBranch()
	out, ok := s.run("scripts/merge.sh", []string{"GH_SCENARIO=notfound"})
	if ok {
		t.Fatalf("merge.sh succeeded without a CI run:\n%s", out)
	}
	if s.exists("ghstate/merged") {
		t.Fatalf("no CI run, but the PR was merged:\n%s", out)
	}
	if want := "no gate.yml run for " + sha; !strings.Contains(out, want) {
		t.Errorf("output lacks %q:\n%s", want, out)
	}
}

func TestMergeSeveralRunsIsLoud(t *testing.T) {
	s := newSandbox(t)
	s.featureBranch()
	out, ok := s.run("scripts/merge.sh", []string{"GH_SCENARIO=several"})
	if ok || s.exists("ghstate/merged") {
		t.Fatalf("merge.sh must refuse when two runs match:\n%s", out)
	}
	if !strings.Contains(out, "several gate.yml runs") {
		t.Errorf("output lacks the ambiguity error:\n%s", out)
	}
}

func TestMergeRefusesBranchBehindMain(t *testing.T) {
	s := newSandbox(t)
	s.featureBranch()
	// main moves on without the branch
	s.git(s.work, "switch", "-q", "main")
	s.write("other.txt", "y\n")
	s.git(s.work, "add", "-A")
	s.git(s.work, "commit", "-q", "-m", "other")
	s.git(s.work, "push", "-q", "origin", "main")
	s.git(s.work, "switch", "-q", "feat")

	out, ok := s.run("scripts/merge.sh", []string{"GH_SCENARIO=green"})
	if ok {
		t.Fatalf("merge.sh accepted a branch behind main:\n%s", out)
	}
	if !strings.Contains(out, "branch is behind main — run: git merge origin/main") {
		t.Errorf("output lacks the behind-main refusal:\n%s", out)
	}
	if s.ghLog() != "" {
		t.Errorf("gh was called before the refusal:\n%s", s.ghLog())
	}
}

// remoteTags lists the tags of the bare origin.
func (s *sandbox) remoteTags() string {
	return s.git(s.root, "--git-dir", s.origin, "tag", "-l")
}

func TestReleaseRefusesWhenHeadIsNotOriginMain(t *testing.T) {
	s := newSandbox(t)
	s.write("local.txt", "z\n")
	s.git(s.work, "add", "-A")
	s.git(s.work, "commit", "-q", "-m", "unpushed")
	out, ok := s.run("scripts/release.sh", nil, "patch")
	if ok {
		t.Fatalf("release.sh released an unpushed HEAD:\n%s", out)
	}
	if !strings.Contains(out, "is not origin/main") {
		t.Errorf("output lacks the refusal:\n%s", out)
	}
	if tags := s.git(s.work, "tag", "-l") + s.remoteTags(); tags != "" {
		t.Errorf("tags were created: %q", tags)
	}
}

// tagOriginMain tags origin/main as tag on the remote: the last release.
func (s *sandbox) tagOriginMain(tag string) {
	s.git(s.work, "tag", tag, "origin/main")
	s.git(s.work, "push", "-q", "origin", tag)
}

func TestReleaseFromBranchMergesThenTags(t *testing.T) {
	s := newSandbox(t)
	s.tagOriginMain("v1.2.3")
	sha := s.featureBranch()
	out, ok := s.run("scripts/release.sh", []string{"GH_SCENARIO=green", "FAKE_UNAME=Linux"}, "minor")
	if !ok {
		t.Fatalf("release.sh failed:\n%s", out)
	}
	if !s.exists("ghstate/merged") || !strings.Contains(s.ghLog(), "gh pr merge feat --merge --match-head-commit "+sha) {
		t.Fatalf("the branch was not merged through its PR:\n%s\n%s", out, s.ghLog())
	}
	s.git(s.work, "fetch", "-q", "origin")
	tagged := s.git(s.work, "rev-parse", "v1.3.0^{commit}")
	if want := s.git(s.work, "rev-parse", "origin/main"); tagged != want {
		t.Errorf("v1.3.0 is %s, want the merge commit on origin/main %s", tagged, want)
	}
	if parents := s.git(s.work, "rev-list", "--parents", "-n1", tagged); !strings.HasSuffix(parents, " "+sha) {
		t.Errorf("the tagged commit is not the merge of %s: %s", sha, parents)
	}
	if !s.exists("ghstate/release") {
		t.Error("no GitHub release was created")
	}
	if b, _ := os.ReadFile(filepath.Join(s.root, "go.log")); strings.Count(string(b), "go test") != 1 {
		t.Errorf("the fast checks should run once (in merge.sh), go log:\n%s", b)
	}
}

func TestReleaseFromBranchRedGateTagsNothing(t *testing.T) {
	s := newSandbox(t)
	s.featureBranch()
	out, ok := s.run("scripts/release.sh", []string{"GH_SCENARIO=red"}, "patch")
	if ok || !strings.Contains(out, "nothing tagged") {
		t.Fatalf("release.sh must stop when the merge fails:\n%s", out)
	}
	if s.exists("ghstate/merged") || s.exists("ghstate/release") || s.remoteTags() != "" {
		t.Errorf("merged, released or tagged on a red gate: tags %q\n%s", s.remoteTags(), out)
	}
}

func TestReleaseFromBranchRefusesWhenMainMoved(t *testing.T) {
	s := newSandbox(t)
	s.featureBranch()
	out, ok := s.run("scripts/release.sh", []string{"GH_SCENARIO=green", "GH_MAIN_MOVED=1"}, "patch")
	if ok || !strings.Contains(out, "main moved in between") {
		t.Fatalf("release.sh must refuse to tag a merge commit with other content:\n%s", out)
	}
	if s.remoteTags() != "" || s.exists("ghstate/release") {
		t.Errorf("tagged or released although main moved: %q", s.remoteTags())
	}
}

func TestReleaseFromBranchBadBumpPushesNothing(t *testing.T) {
	s := newSandbox(t)
	s.featureBranch()
	out, ok := s.run("scripts/release.sh", []string{"GH_SCENARIO=green"}, "huge")
	if ok || !strings.Contains(out, "Usage: make release") {
		t.Fatalf("release.sh must refuse an unknown bump:\n%s", out)
	}
	if s.ghLog() != "" {
		t.Errorf("gh was called before the bump was checked:\n%s", s.ghLog())
	}
}

func TestReleaseTagsNothingWhenLocalGateFails(t *testing.T) {
	s := newSandbox(t)
	// The local gate is scripts/test.sh all: its DB layer fails here.
	s.write("e2e/db-test.sh", "#!/usr/bin/env bash\necho 'db tests failed' >&2\nexit 1\n")
	s.git(s.work, "add", "-A")
	s.git(s.work, "commit", "-q", "-m", "a failing db layer")
	s.git(s.work, "push", "-q", "origin", "main")

	out, ok := s.run("scripts/release.sh", []string{"FAKE_UNAME=Darwin"}, "minor")
	if ok {
		t.Fatalf("release.sh succeeded although the local gate failed:\n%s", out)
	}
	if !strings.Contains(out, "local gate (macOS)") {
		t.Errorf("the local gate did not run:\n%s", out)
	}
	if tags := s.git(s.work, "tag", "-l") + s.remoteTags(); tags != "" {
		t.Errorf("tags were created although the gate failed: %q", tags)
	}
	if s.exists("ghstate/release") {
		t.Error("a GitHub release was created although the gate failed")
	}
}

func TestReleaseOnLinuxTagsOriginMain(t *testing.T) {
	s := newSandbox(t)
	s.write("e2e/db-test.sh", "#!/usr/bin/env bash\necho 'the local gate must not run on Linux' >&2\nexit 1\n")
	s.git(s.work, "add", "-A")
	s.git(s.work, "commit", "-q", "-m", "work")
	s.git(s.work, "push", "-q", "origin", "main")
	s.git(s.work, "tag", "v1.2.3", "HEAD~1")
	s.git(s.work, "push", "-q", "origin", "v1.2.3")

	out, ok := s.run("scripts/release.sh", []string{"FAKE_UNAME=Linux"}, "minor")
	if !ok {
		t.Fatalf("release.sh failed:\n%s", out)
	}
	if !strings.Contains(s.remoteTags(), "v1.3.0") {
		t.Errorf("v1.3.0 was not pushed; remote tags: %q\n%s", s.remoteTags(), out)
	}
	if got, want := s.git(s.work, "rev-parse", "v1.3.0^{commit}"), s.git(s.work, "rev-parse", "origin/main"); got != want {
		t.Errorf("v1.3.0 is %s, want origin/main %s", got, want)
	}
	if !s.exists("ghstate/release") {
		t.Error("no GitHub release was created")
	}

	// Nothing new since v1.3.0: refused.
	out, ok = s.run("scripts/release.sh", nil, "patch")
	if ok || !strings.Contains(out, "nothing to release") {
		t.Fatalf("release.sh must refuse with nothing since the last tag:\n%s", out)
	}
}

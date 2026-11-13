package ws

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func remoteURL(t *testing.T, path string) string {
	t.Helper()
	return mustGit(t, path, "remote", "get-url", "origin")
}

func remoteBranches(t *testing.T, remote string) string {
	t.Helper()
	return mustGit(t, remote, "for-each-ref", "--format=%(refname)=%(objectname)", "refs/heads")
}

func advanceRemote(t *testing.T, remote, filename string) {
	t.Helper()
	clone := filepath.Join(t.TempDir(), "clone")
	mustGit(t, filepath.Dir(clone), "clone", remote, clone)
	mustGit(t, clone, "config", "user.name", "Workspace Steward Test")
	mustGit(t, clone, "config", "user.email", "ws@example.test")
	commitFile(t, clone, filename, "remote\n")
	mustGit(t, clone, "push", "origin", "main")
}

func TestPushSkipsMissingOriginAndContinues(t *testing.T) {
	e := fixture(t, 3)
	for _, name := range []string{"repo0", "repo1", "repo2"} {
		commitFile(t, filepath.Join(e.Root, name), "publish.txt", name+"\n")
	}
	missing := filepath.Join(e.Root, "repo1")
	mustGit(t, missing, "remote", "remove", "origin")
	mustGit(t, e.Root, "remote", "remove", "origin")
	var output bytes.Buffer
	e.Out = &output
	if err := e.Push(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"repo0", "repo2"} {
		path := filepath.Join(e.Root, name)
		if got, want := mustGit(t, remoteURL(t, path), "rev-parse", "main"), mustGit(t, path, "rev-parse", "main"); got != want {
			t.Fatalf("%s remote = %s, want %s", name, got, want)
		}
	}
	log := output.String()
	previous := -1
	for _, want := range []string{
		"[push/repo0 @ " + filepath.Join(e.Root, "repo0") + "] pushed trunk main",
		"[push/repo1 @ " + missing + "] skipped: no origin remote",
		"[push/repo2 @ " + filepath.Join(e.Root, "repo2") + "] pushed trunk main",
		"[push/root @ " + e.Root + "] skipped: no origin remote",
	} {
		if index := strings.Index(log, want); index <= previous {
			t.Fatalf("missing or out-of-order push notice %q:\n%s", want, log)
		} else {
			previous = index
		}
	}
}

func TestPushPublishesDependenciesBeforeRoot(t *testing.T) {
	e := fixture(t, 2)
	e.Config.Children[0].DependsOn = []string{"repo1"}
	commitFile(t, filepath.Join(e.Root, "repo0"), "local.txt", "repo0\n")
	commitFile(t, filepath.Join(e.Root, "repo1"), "local.txt", "repo1\n")
	commitFile(t, e.Root, "local.txt", "root\n")

	var output bytes.Buffer
	e.Out = &output
	if err := e.Push(); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(e.Root, "repo0"), filepath.Join(e.Root, "repo1"), e.Root} {
		remote := remoteURL(t, path)
		if got, want := mustGit(t, remote, "rev-parse", "refs/heads/main"), mustGit(t, path, "rev-parse", "refs/heads/main"); got != want {
			t.Fatalf("remote %s = %s, want %s", remote, got, want)
		}
		if got, want := mustGit(t, path, "rev-parse", "refs/remotes/origin/main"), mustGit(t, path, "rev-parse", "refs/heads/main"); got != want {
			t.Fatalf("cached origin/main for %s = %s, want %s", path, got, want)
		}
	}
	report, err := e.Tree(e.Root)
	if err != nil {
		t.Fatal(err)
	}
	for _, repository := range report.Repositories {
		if repository.SyncState != "current" || repository.Ahead != 0 || repository.Behind != 0 {
			t.Fatalf("repository %s status after push: %+v", repository.Name, repository)
		}
	}
	log := output.String()
	repo1 := strings.Index(log, "[push/repo1 @ "+filepath.Join(e.Root, "repo1")+"] pushed trunk main")
	repo0 := strings.Index(log, "[push/repo0 @ "+filepath.Join(e.Root, "repo0")+"] pushed trunk main")
	root := strings.Index(log, "[push/root @ "+e.Root+"] pushed trunk main")
	if repo1 < 0 || repo0 < repo1 || root < repo0 {
		t.Fatalf("push order does not follow dependencies with root last:\n%s", log)
	}
}

func TestPushCreatesRemoteTrunkFromDirtyCheckoutOnAnotherBranch(t *testing.T) {
	e := fixture(t, 1)
	path := filepath.Join(e.Root, "repo0")
	remote := remoteURL(t, path)
	commitFile(t, path, "trunk.txt", "trunk\n")
	trunk := mustGit(t, path, "rev-parse", "refs/heads/main")
	mustGit(t, remote, "update-ref", "-d", "refs/heads/main")
	mustGit(t, path, "checkout", "-b", "topic")
	commitFile(t, path, "topic.txt", "topic\n")
	put(t, filepath.Join(path, "dirty.txt"), "dirty\n")

	if err := e.Push(); err != nil {
		t.Fatal(err)
	}
	if got := mustGit(t, remote, "rev-parse", "refs/heads/main"); got != trunk {
		t.Fatalf("published %s, want configured trunk %s", got, trunk)
	}
}

func TestPushStopsAtInvalidCheckoutOrDestination(t *testing.T) {
	tests := map[string]struct {
		setup func(t *testing.T, path string)
		want  string
	}{
		"nested non-checkout": {
			setup: func(t *testing.T, path string) {
				if err := os.Rename(path, path+"-moved"); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(path, 0o755); err != nil {
					t.Fatal(err)
				}
			},
			want: "path is not a Git checkout root",
		},
		"mismatched origin": {
			setup: func(t *testing.T, path string) {
				mustGit(t, path, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "wrong.git"))
			},
			want: "origin mismatch",
		},
		"multiple push URLs": {
			setup: func(t *testing.T, path string) {
				mustGit(t, path, "remote", "set-url", "--push", "origin", remoteURL(t, path))
				mustGit(t, path, "remote", "set-url", "--push", "--add", "origin", filepath.Join(t.TempDir(), "wrong.git"))
			},
			want: "exactly one origin push URL",
		},
		"non-fast-forward": {
			setup: func(t *testing.T, path string) {
				commitFile(t, path, "local.txt", "local\n")
				advanceRemote(t, remoteURL(t, path), "remote.txt")
			},
			want: "failed to push",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			e := fixture(t, 3)
			first := filepath.Join(e.Root, "repo0")
			missing := filepath.Join(e.Root, "repo1")
			blocked := filepath.Join(e.Root, "repo2")
			commitFile(t, first, "candidate.txt", "child\n")
			firstRevision := mustGit(t, first, "rev-parse", "refs/heads/main")
			commitFile(t, e.Root, "candidate.txt", "root\n")
			mustGit(t, missing, "remote", "remove", "origin")
			tc.setup(t, blocked)
			blockedRemote := e.Config.Children[2].URL
			blockedBefore := remoteBranches(t, blockedRemote)
			rootRemote := remoteURL(t, e.Root)
			rootBefore := remoteBranches(t, rootRemote)

			err := e.Push()
			var result *PublicationError
			if !errors.As(err, &result) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected push failure containing %q, got %v", tc.want, err)
			}
			if result.Blocked != "repo2" || !slices.Equal(result.Completed, []string{"repo0"}) || !slices.Equal(result.Pending, []string{"root"}) {
				t.Fatalf("unexpected publication progress: %+v", result)
			}
			if got := mustGit(t, remoteURL(t, first), "rev-parse", "refs/heads/main"); got != firstRevision {
				t.Fatalf("earlier push did not publish: got %s, want %s", got, firstRevision)
			}
			if got := remoteBranches(t, blockedRemote); got != blockedBefore {
				t.Fatalf("blocked repository changed: %q -> %q", blockedBefore, got)
			}
			if got := remoteBranches(t, rootRemote); got != rootBefore {
				t.Fatalf("unvisited root changed: %q -> %q", rootBefore, got)
			}
		})
	}
}

func TestPushIsIdempotentAndRetryableAfterPartialPublication(t *testing.T) {
	e := fixture(t, 1)
	child := filepath.Join(e.Root, "repo0")
	childRemote := remoteURL(t, child)
	rootRemote := remoteURL(t, e.Root)
	rootBefore := mustGit(t, rootRemote, "rev-parse", "refs/heads/main")
	commitFile(t, child, "candidate.txt", "child\n")
	commitFile(t, e.Root, "candidate.txt", "root\n")

	hook := filepath.Join(rootRemote, "hooks", "pre-receive")
	put(t, hook, "#!/bin/sh\nexit 1\n")
	if err := os.Chmod(hook, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := e.Push(); err == nil {
		t.Fatal("root remote rejection did not fail push")
	}
	if got, want := mustGit(t, childRemote, "rev-parse", "refs/heads/main"), mustGit(t, child, "rev-parse", "HEAD"); got != want {
		t.Fatalf("child was not published before root failure: got %s, want %s", got, want)
	}
	if got := mustGit(t, rootRemote, "rev-parse", "refs/heads/main"); got != rootBefore {
		t.Fatalf("rejected root unexpectedly changed: got %s, want %s", got, rootBefore)
	}
	if err := os.Remove(hook); err != nil {
		t.Fatal(err)
	}
	if err := e.Push(); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if err := e.Push(); err != nil {
		t.Fatalf("already-current retry: %v", err)
	}
	if got, want := mustGit(t, rootRemote, "rev-parse", "refs/heads/main"), mustGit(t, e.Root, "rev-parse", "HEAD"); got != want {
		t.Fatalf("root retry did not publish: got %s, want %s", got, want)
	}
}

func TestPushReportsAlreadyCurrentTrunksAsUnchanged(t *testing.T) {
	e := fixture(t, 1)
	if err := e.Push(); err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	e.Out = &output
	if err := e.Push(); err != nil {
		t.Fatal(err)
	}
	for _, repository := range []string{"repo0", "root"} {
		path := e.Root
		if repository != "root" {
			path = filepath.Join(e.Root, repository)
		}
		if want := "[push/" + repository + " @ " + path + "] trunk main unchanged at "; !strings.Contains(output.String(), want) {
			t.Fatalf("push output missing %q:\n%s", want, output.String())
		}
	}
	if strings.Contains(output.String(), " pushed trunk main") {
		t.Fatalf("already-current push reported publication:\n%s", output.String())
	}
}

func TestPushPlanShowsSequentialPushesAndSkips(t *testing.T) {
	e := fixture(t, 2)
	e.Config.Children[0].DependsOn = []string{"repo1"}
	pushPath := filepath.Join(e.Root, "repo1")
	remote := remoteURL(t, pushPath)
	commitFile(t, pushPath, "publish.txt", "local work")
	mustGit(t, filepath.Join(e.Root, "repo0"), "remote", "remove", "origin")
	mustGit(t, e.Root, "remote", "remove", "origin")
	trackingBefore := mustGit(t, pushPath, "rev-parse", "refs/remotes/origin/main")
	fetchHeadPath := mustGit(t, pushPath, "rev-parse", "--git-path", "FETCH_HEAD")
	if !filepath.IsAbs(fetchHeadPath) {
		fetchHeadPath = filepath.Join(pushPath, fetchHeadPath)
	}
	// Keep the remote ahead of the cached tracking ref so a fetch is observable.
	advanceRemote(t, remote, "remote.txt")
	remoteBefore := remoteBranches(t, remote)

	plan := e.Plan("push", nil)
	if len(plan.Blockers) != 0 || len(plan.Steps) != 3 {
		t.Fatalf("unexpected push preview: %+v", plan)
	}
	for i, want := range []struct{ name, checkpoint string }{
		{"repo1", "pending"},
		{"repo0", "skipped"},
		{"root", "skipped"},
	} {
		step := plan.Steps[i]
		if step.Phase != "push" || step.Repository != want.name || step.Checkpoint != want.checkpoint {
			t.Fatalf("unexpected step %d: %+v", i, step)
		}
	}
	if got := remoteBranches(t, remote); got != remoteBefore {
		t.Fatalf("dry run changed remote refs: %q -> %q", remoteBefore, got)
	}
	if got := mustGit(t, pushPath, "rev-parse", "refs/remotes/origin/main"); got != trackingBefore {
		t.Fatalf("dry run changed origin/main: %s -> %s", trackingBefore, got)
	}
	if _, err := os.Stat(fetchHeadPath); !os.IsNotExist(err) {
		t.Fatalf("expected no FETCH_HEAD after dry run, stat error: %v", err)
	}
}

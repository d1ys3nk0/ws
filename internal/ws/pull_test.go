package ws

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func advancePullRemote(t *testing.T, url, filename string) string {
	t.Helper()
	clone := filepath.Join(t.TempDir(), "clone")
	mustGit(t, filepath.Dir(clone), "clone", url, clone)
	commitFile(t, clone, filename, filename+"\n")
	mustGit(t, clone, "push", "origin", "main")
	return mustGit(t, clone, "rev-parse", "HEAD")
}

func TestSyncFetchesRootAndChildrenWithoutChangingWorktrees(t *testing.T) {
	e := fixture(t, 1)
	child := filepath.Join(e.Root, "repo0")
	rootBefore, childBefore := mustGit(t, e.Root, "rev-parse", "HEAD"), mustGit(t, child, "rev-parse", "HEAD")
	rootURL := mustGit(t, e.Root, "remote", "get-url", "origin")
	rootRemote := advancePullRemote(t, rootURL, "root-remote.txt")
	childRemote := advancePullRemote(t, e.Config.Children[0].URL, "child-remote.txt")
	put(t, filepath.Join(child, "untracked.txt"), "preserved\n")
	if err := e.Fetch(); err != nil {
		t.Fatal(err)
	}
	if got, _ := head(e.Root); got != rootBefore {
		t.Fatalf("sync moved root HEAD: %s -> %s", rootBefore, got)
	}
	if got, _ := head(child); got != childBefore {
		t.Fatalf("sync moved child HEAD: %s -> %s", childBefore, got)
	}
	if got := mustGit(t, e.Root, "rev-parse", "refs/remotes/origin/main"); got != rootRemote {
		t.Fatalf("root cached ref = %s, want %s", got, rootRemote)
	}
	if got := mustGit(t, child, "rev-parse", "refs/remotes/origin/main"); got != childRemote {
		t.Fatalf("child cached ref = %s, want %s", got, childRemote)
	}
	if _, err := os.Stat(filepath.Join(child, "untracked.txt")); err != nil {
		t.Fatalf("sync changed child worktree: %v", err)
	}
}

func TestPullRebasesCanonicalLocalCommits(t *testing.T) {
	e := fixture(t, 1)
	child := filepath.Join(e.Root, "repo0")
	commitFile(t, child, "local.txt", "local\n")
	localBefore := mustGit(t, child, "rev-parse", "HEAD")
	advancePullRemote(t, e.Config.Children[0].URL, "remote.txt")
	if _, err := e.Pull(); err != nil {
		t.Fatal(err)
	}
	after := mustGit(t, child, "rev-parse", "HEAD")
	if after == localBefore || !ancestor(child, "refs/remotes/origin/main", after) {
		t.Fatalf("local commit was not rebased: before %s after %s", localBefore, after)
	}
	for _, filename := range []string{"local.txt", "remote.txt"} {
		if _, err := os.Stat(filepath.Join(child, filename)); err != nil {
			t.Fatalf("rebased checkout omitted %s: %v", filename, err)
		}
	}
}

func TestPullBlocksRewritingActiveChangeBaseline(t *testing.T) {
	e := fixture(t, 1)
	child := filepath.Join(e.Root, "repo0")
	commitFile(t, child, "local.txt", "local\n")
	manifest, err := e.Create("baseline-guard")
	if err != nil {
		t.Fatal(err)
	}
	before := mustGit(t, child, "rev-parse", "HEAD")
	advancePullRemote(t, e.Config.Children[0].URL, "remote.txt")
	_, err = e.Pull()
	if err == nil || !strings.Contains(err.Error(), "repository repo0 pull would rewrite commits recorded as baselines by active managed workspaces: "+manifest.WorkspaceID) {
		t.Fatalf("unexpected baseline guard error: %v", err)
	}
	if after := mustGit(t, child, "rev-parse", "HEAD"); after != before {
		t.Fatalf("blocked pull moved child trunk: %s -> %s", before, after)
	}
}

func TestPullUpdatesChildWhenRootIsDirty(t *testing.T) {
	e := fixture(t, 1)
	child := filepath.Join(e.Root, "repo0")
	remote := advancePullRemote(t, e.Config.Children[0].URL, "remote.txt")
	put(t, filepath.Join(e.Root, "dirty.txt"), "dirty\n")
	if _, err := e.Pull(); err == nil || !strings.Contains(err.Error(), "repository root pull preflight") {
		t.Fatalf("unexpected preflight error: %v", err)
	}
	if after := mustGit(t, child, "rev-parse", "HEAD"); after != remote {
		t.Fatalf("child trunk = %s, want %s", after, remote)
	}
}

func TestPullFromChangeUpdatesOnlyCanonicalTrunks(t *testing.T) {
	e := fixture(t, 1)
	manifest, err := e.Create("pull-from-change")
	if err != nil {
		t.Fatal(err)
	}
	changeBefore := mustGit(t, manifest.Repositories[1].Path, "rev-parse", "HEAD")
	remote := advancePullRemote(t, e.Config.Children[0].URL, "remote.txt")
	fromChange, err := Open(manifest.Workspace, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = fromChange.Pull(); err != nil {
		t.Fatal(err)
	}
	if canonical := mustGit(t, manifest.Repositories[1].Origin, "rev-parse", "HEAD"); canonical != remote {
		t.Fatalf("canonical trunk = %s, want %s", canonical, remote)
	}
	if change := mustGit(t, manifest.Repositories[1].Path, "rev-parse", "HEAD"); change != changeBefore {
		t.Fatalf("pull refreshed Change worktree: %s -> %s", changeBefore, change)
	}
}

func TestPullPreservesInterruptedRebaseAndContinues(t *testing.T) {
	e := fixture(t, 1)
	child := filepath.Join(e.Root, "repo0")
	put(t, filepath.Join(child, "file.txt"), "local\n")
	mustGit(t, child, "add", "file.txt")
	mustGit(t, child, "commit", "-m", "feat: local conflict")
	clone := filepath.Join(t.TempDir(), "clone")
	mustGit(t, filepath.Dir(clone), "clone", e.Config.Children[0].URL, clone)
	put(t, filepath.Join(clone, "file.txt"), "remote\n")
	mustGit(t, clone, "add", "file.txt")
	mustGit(t, clone, "commit", "-m", "feat: remote conflict")
	mustGit(t, clone, "push", "origin", "main")
	rootURL := mustGit(t, e.Root, "remote", "get-url", "origin")
	rootRemote := advancePullRemote(t, rootURL, "root-remote.txt")
	_, err := e.Pull()
	if err == nil || !strings.Contains(err.Error(), "repository repo0 pull rebase interrupted") {
		t.Fatalf("unexpected rebase conflict error: %v", err)
	}
	rebasePath := mustGit(t, child, "rev-parse", "--git-path", "rebase-merge")
	if !filepath.IsAbs(rebasePath) {
		rebasePath = filepath.Join(child, rebasePath)
	}
	if _, err := os.Stat(rebasePath); err != nil {
		t.Fatalf("interrupted rebase was not preserved: %v", err)
	}
	if rootAfter := mustGit(t, e.Root, "rev-parse", "HEAD"); rootAfter != rootRemote {
		t.Fatalf("root trunk = %s after child conflict, want %s", rootAfter, rootRemote)
	}
}

func TestPullClonesChildrenAfterManualRootClone(t *testing.T) {
	seed := fixture(t, 1)
	child := seed.Config.Children[0]
	mustGit(t, filepath.Join(seed.Root, child.Path), "push", "origin", "main:stable")
	config := strings.ReplaceAll(mustGit(t, seed.Root, "show", "HEAD:workspace.yml"), child.URL, "../repo0.git")
	config = strings.Replace(config, "    trunk: main", "    trunk: stable", 1)
	commitFile(t, seed.Root, "workspace.yml", config+"\n")
	mustGit(t, seed.Root, "push", "origin", "main")

	dir := filepath.Dir(seed.Root)
	root := filepath.Join(dir, "manual")
	mustGit(t, dir, "clone", filepath.Join(dir, "workspace.git"), root)
	e, err := Open(root, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	revision := mustGit(t, child.URL, "rev-parse", "stable")
	results, err := e.Pull()
	if err != nil || len(results) != 2 {
		t.Fatalf("first pull = %+v, %v", results, err)
	}
	want := PullRepositoryResult{Name: child.Name, Path: filepath.Join(root, child.Path), Trunk: "stable", Outcome: PullCloned, After: revision}
	if results[0] != want || results[1].Name != "root" || results[1].Outcome != PullUnchanged {
		t.Fatalf("first pull = %+v, want cloned child %+v and unchanged root", results, want)
	}
	results, err = e.Pull()
	if err != nil || len(results) != 2 || results[0].Outcome != PullUnchanged || results[1].Outcome != PullUnchanged {
		t.Fatalf("second pull = %+v, %v", results, err)
	}
}

func TestPullContinuesAfterCloneAndFetchFailures(t *testing.T) {
	e := fixture(t, 4)
	cloneFailure, fetchFailure, occupied, healthy := e.Config.Children[0], e.Config.Children[1], e.Config.Children[2], e.Config.Children[3]
	for _, path := range []string{filepath.Join(e.Root, cloneFailure.Path), cloneFailure.URL, fetchFailure.URL, filepath.Join(e.Root, occupied.Path)} {
		if err := os.RemoveAll(path); err != nil {
			t.Fatal(err)
		}
	}
	occupiedPath := filepath.Join(e.Root, occupied.Path)
	put(t, occupiedPath, "preserve me")
	mustGit(t, e.Root, "remote", "remove", "origin")
	wantHead := advancePullRemote(t, healthy.URL, "remote.txt")

	results, err := e.Pull()
	if err == nil || len(results) != 5 {
		t.Fatalf("pull = %+v, %v", results, err)
	}
	for index, want := range []struct {
		name    string
		outcome PullOutcome
		detail  string
	}{
		{cloneFailure.Name, PullFailed, "clone:"},
		{fetchFailure.Name, PullFailed, "fetch remote trunk"},
		{occupied.Name, PullFailed, "preflight:"},
		{healthy.Name, PullUpdated, ""},
		{"root", PullFailed, "resolve origin"},
	} {
		got := results[index]
		if got.Name != want.name || got.Outcome != want.outcome || !strings.Contains(got.Error, want.detail) {
			t.Errorf("result %d = %+v, want %+v", index, got, want)
		}
	}
	if got := mustGit(t, filepath.Join(e.Root, healthy.Path), "rev-parse", "HEAD"); got != wantHead {
		t.Errorf("healthy child HEAD = %s, want %s", got, wantHead)
	}
	if got, err := os.ReadFile(occupiedPath); err != nil || string(got) != "preserve me" {
		t.Errorf("occupied checkout path changed: %q, %v", got, err)
	}
}

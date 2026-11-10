package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/d1ys3nk0/ws/internal/ws"
)

func cloneSource(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	extendedGit(t, dir, "init", "-b", "main", path)
	cloneCommit(t, path)
	return path
}

func cloneCommit(t *testing.T, path string) {
	t.Helper()
	extendedGit(t, path, "add", "--all")
	extendedGit(t, path, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-m", "fixture")
}

func writeCloneConfig(t *testing.T, root, config string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "workspace.yml"), []byte(config), 0644); err != nil {
		t.Fatal(err)
	}
	cloneCommit(t, root)
}

func TestCloneCLIConfiguredRepositories(t *testing.T) {
	sources, dest := t.TempDir(), t.TempDir()
	t.Chdir(dest)
	root := cloneSource(t, sources, "project.git")
	dependency := cloneSource(t, sources, "dependency")
	extendedGit(t, dependency, "branch", "stable")
	child := cloneSource(t, sources, "child")
	writeCloneConfig(t, root, fmt.Sprintf(`version: 1
root:
  trunk: alternate
  hooks:
    create-after:
      - id: marker
        shell: touch hook-ran
children:
  - name: child
    path: components/child
    url: %q
    trunk: main
    depends_on: [dependency]
  - name: dependency
    path: components/dependency
    url: %q
    trunk: stable
`, child, dependency))

	got, err := runJSON[ws.CloneResult](t, "clone", root, "--json")
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := filepath.EvalSymlinks(dest)
	if err != nil {
		t.Fatal(err)
	}
	if got.Root != filepath.Join(canonical, "project") || got.Warning != "" || len(got.Repositories) != 2 {
		t.Fatalf("unexpected clone result: %+v", got)
	}
	if got.Repositories[0].Name != "dependency" || got.Repositories[1].Name != "child" {
		t.Fatalf("unexpected dependency order: %+v", got.Repositories)
	}
	if branch := extendedGit(t, got.Root, "branch", "--show-current"); branch != "main" {
		t.Fatalf("root branch = %q, want main", branch)
	}
	for _, repo := range got.Repositories {
		if repo.Path != filepath.Join(got.Root, "components", repo.Name) || repo.Outcome != "cloned" {
			t.Fatalf("unexpected repository: %+v", repo)
		}
	}
	if branch := extendedGit(t, got.Repositories[0].Path, "branch", "--show-current"); branch != "stable" {
		t.Fatalf("dependency branch = %q, want stable", branch)
	}
	if _, err := os.Stat(filepath.Join(got.Root, "hook-ran")); !os.IsNotExist(err) {
		t.Fatalf("unexpected hook marker: %v", err)
	}
}

func TestCloneCLIWithoutConfig(t *testing.T) {
	sources := t.TempDir()
	t.Chdir(t.TempDir())
	root := cloneSource(t, sources, "plain")
	out, stderr, err := runCapturedOutput(t, "clone", root, "nested/plain checkout")
	if err != nil || !strings.Contains(out, "Cloned root:") || !strings.Contains(stderr, "Warning: workspace.yml not found") {
		t.Fatalf("root-only clone: stdout=%q stderr=%q err=%v", out, stderr, err)
	}
	extendedGit(t, "nested/plain checkout", "rev-parse", "--verify", "HEAD")
	if _, err := runOutput(t, "clone", root, "nested/plain checkout"); err == nil {
		t.Fatal("cloned over existing checkout")
	}
	extendedGit(t, "nested/plain checkout", "rev-parse", "--verify", "HEAD")

	got, err := runJSON[ws.CloneResult](t, "clone", root, filepath.Join(t.TempDir(), "absolute"), "--json")
	if err != nil || !filepath.IsAbs(got.Root) || !strings.Contains(got.Warning, "workspace.yml not found") || got.Repositories == nil || len(got.Repositories) != 0 {
		t.Fatalf("root-only JSON: result=%+v err=%v", got, err)
	}
}

func TestCloneCLIInvalidConfig(t *testing.T) {
	sources, dest := t.TempDir(), t.TempDir()
	root := cloneSource(t, sources, "invalid")
	child := cloneSource(t, sources, "child")
	writeCloneConfig(t, root, fmt.Sprintf(`version: 1
root: {trunk: main}
children:
  - {name: child, path: child, url: %q, trunk: main}
  - {name: invalid, path: ../escape, url: %q, trunk: main}
`, child, child))
	checkout := filepath.Join(dest, "invalid")
	_, stderr, err := runCapturedOutput(t, "clone", root, checkout)
	if err == nil || !strings.Contains(err.Error(), "invalid repository path") || strings.Contains(stderr, "not found") {
		t.Fatalf("invalid config: stderr=%q err=%v", stderr, err)
	}
	extendedGit(t, checkout, "rev-parse", "--verify", "HEAD")
	if _, err := os.Stat(filepath.Join(checkout, "child")); !os.IsNotExist(err) {
		t.Fatalf("child was cloned before config validation: %v", err)
	}
}

func TestCloneCLILocalBundle(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	source := cloneSource(t, dir, "source")
	extendedGit(t, source, "bundle", "create", filepath.Join(dir, "archive.bundle"), "--all")

	got, err := runJSON[ws.CloneResult](t, "clone", "archive.bundle", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(got.Root) != "archive" {
		t.Fatalf("bundle checkout = %q, want archive", got.Root)
	}
	extendedGit(t, got.Root, "rev-parse", "--verify", "HEAD")
}

func TestCloneCLIResumesWithBootstrap(t *testing.T) {
	dir := t.TempDir()
	root := cloneSource(t, dir, "root")
	cloneSource(t, dir, "first")
	writeCloneConfig(t, root, `version: 1
root: {trunk: main}
children:
  - {name: first, path: first, url: ../first, trunk: main}
  - {name: second, path: second, url: ../second, trunk: main, depends_on: [first]}
`)
	checkout := filepath.Join(dir, "partial")
	out, stderr, err := runCapturedOutput(t, "clone", root, checkout)
	if err == nil || !strings.Contains(err.Error(), "repository second") || !strings.Contains(err.Error(), "ws bootstrap") {
		t.Fatalf("partial clone error = %v", err)
	}
	if !strings.Contains(out, "Cloned root:") || strings.Contains(out, "first: cloned") || !strings.Contains(stderr, "first: cloned") {
		t.Fatalf("partial clone: stdout=%q stderr=%q", out, stderr)
	}
	second := cloneSource(t, dir, "second")
	if _, _, err := runCapturedOutput(t, "bootstrap", "--workspace", checkout); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"first", "second"} {
		extendedGit(t, filepath.Join(checkout, name), "rev-parse", "--verify", "HEAD")
	}
	extendedGit(t, filepath.Join(checkout, "first"), "remote", "set-url", "origin", second)
	if _, _, err := runCapturedOutput(t, "bootstrap", "--workspace", checkout); err == nil || !strings.Contains(err.Error(), "origin mismatch") {
		t.Fatalf("bootstrap accepted a different origin: %v", err)
	}
}

func TestCloneCLIHelpAndCompletion(t *testing.T) {
	_, help, err := runCapturedOutput(t, "clone", "--help")
	if err != nil || !strings.Contains(help, "ws clone <remote> [destination]") {
		t.Fatalf("help=%q err=%v", help, err)
	}
	out, err := runOutput(t, "_complete", "commands")
	if err != nil || !strings.Contains("\n"+out, "\nclone\n") {
		t.Fatalf("commands=%q err=%v", out, err)
	}
}

package ws

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type CloneRepository struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Trunk   string `json:"trunk"`
	Outcome string `json:"outcome"`
}

type CloneResult struct {
	Root         string            `json:"root"`
	Repositories []CloneRepository `json:"repositories"`
	Warning      string            `json:"warning,omitempty"`
}

func Clone(cwd, remote, destination string) (CloneResult, error) {
	result := CloneResult{Repositories: []CloneRepository{}}
	if destination == "" {
		destination = cloneBasename(remote, isLocalBundle(cwd, remote))
		if destination == "" || destination == "." || destination == ".." {
			return result, fmt.Errorf("cannot infer clone destination; specify a destination")
		}
	}
	root := destination
	if !filepath.IsAbs(root) {
		root = cwd + string(filepath.Separator) + root
	}
	if err := cloneRoot(cwd, remote, destination); err != nil {
		return result, err
	}
	result.Root = root
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		return result, err
	}
	result.Root = root

	configPath := filepath.Join(root, "workspace.yml")
	info, err := os.Lstat(configPath)
	if os.IsNotExist(err) {
		result.Warning = "workspace.yml not found; cloned only the root repository"
		return result, nil
	}
	if err != nil {
		return result, failure("configuration", "", err)
	}
	if !info.Mode().IsRegular() {
		return result, failure("configuration", "", fmt.Errorf("%s must be a regular file", configPath))
	}
	config, err := readConfig(root)
	if err != nil {
		return result, failure("configuration", "", fmt.Errorf("%s: %w", configPath, err))
	}
	err = bootstrap(root, config, func(repository CloneRepository) {
		result.Repositories = append(result.Repositories, repository)
	})
	if err != nil {
		return result, fmt.Errorf("%w; fix the problem and run ws bootstrap in %s", err, root)
	}
	return result, nil
}

func cloneRoot(cwd, remote, destination string) error {
	cmd := exec.Command("git", "clone", "--", remote, destination)
	cmd.Dir = cwd
	cmd.Stdin = os.Stdin
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git clone into %q: %s: %w", destination, redactURLCredentials(strings.TrimSpace(stderr.String())), err)
	}
	return nil
}

func isLocalBundle(cwd, remote string) bool {
	path := remote
	if !filepath.IsAbs(path) {
		path = cwd + string(filepath.Separator) + path
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	_, err = git(cwd, "bundle", "list-heads", "--", path)
	return err == nil
}

func cloneBasename(remote string, bundle bool) string {
	if _, rest, ok := strings.Cut(remote, "://"); ok {
		remote = rest
	}
	host, _, _ := strings.Cut(remote, "/")
	if at := strings.LastIndexByte(host, '@'); at >= 0 {
		remote = remote[at+1:]
	}
	remote = strings.TrimRight(remote, "/ \t\n\r\v\f")
	if strings.HasSuffix(remote, "/.git") && len(remote) > len("/.git") {
		remote = strings.TrimRight(strings.TrimSuffix(remote, "/.git"), "/")
	}
	if !strings.Contains(remote, "/") {
		if colon := strings.LastIndexByte(remote, ':'); colon >= 0 && strings.Trim(remote[colon+1:], "0123456789") == "" {
			remote = remote[:colon]
		}
	}
	name := remote[strings.LastIndexAny(remote, "/:")+1:]
	suffix := ".git"
	if bundle {
		suffix = ".bundle"
	}
	name = strings.TrimSuffix(name, suffix)
	return strings.Join(strings.FieldsFunc(name, func(r rune) bool { return r <= ' ' }), " ")
}

func bootstrap(root string, config Config, report func(CloneRepository)) error {
	ordered, err := config.Order()
	if err != nil {
		return err
	}
	for _, r := range ordered {
		path := filepath.Join(root, r.Path)
		outcome, err := bootstrapRepository(root, r, path)
		if err != nil {
			return fmt.Errorf("repository %s bootstrap: %w", r.Name, err)
		}
		report(CloneRepository{Name: r.Name, Path: path, Trunk: r.Trunk, Outcome: outcome})
	}
	return nil
}

func bootstrapRepository(root string, r Repository, path string) (string, error) {
	if _, err := os.Lstat(path); err == nil {
		return "validated", validateOriginFrom(path, r, root)
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return "", err
	}
	if _, err := git(root, "clone", "--branch", r.Trunk, "--", r.URL, path); err != nil {
		return "", fmt.Errorf("%w; remove any incomplete clone after inspection and retry", err)
	}
	return "cloned", nil
}

package ws

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

func (e *Engine) pushRepositories() ([]canonicalRepository, error) {
	ordered, err := e.Config.Order()
	if err != nil {
		return nil, err
	}
	repositories := make([]canonicalRepository, 0, len(ordered)+1)
	for _, repository := range ordered {
		repositories = append(repositories, canonicalRepository{repository: repository, path: filepath.Join(e.Root, repository.Path)})
	}
	return append(repositories, canonicalRepository{
		repository: Repository{Name: "root", Trunk: e.Config.Root.Trunk},
		path:       e.Root,
	}), nil
}

// pushTarget resolves the current local trunk and its destination.
// An empty destination with no error means the checkout has no origin.
func pushTarget(repository canonicalRepository) (revision, destination string, err error) {
	r := repository.repository
	path := repository.path
	if err := validateCheckoutRoot(path, r.Name); err != nil {
		return "", "", err
	}
	remotes, err := git(path, "remote")
	if err != nil {
		return "", "", err
	}
	if !slices.Contains(strings.Split(remotes, "\n"), "origin") {
		return "", "", nil
	}
	if r.Name != "root" {
		if err := validateOriginURL(path, r, ""); err != nil {
			return "", "", err
		}
	}
	destination, err = pushDestination(path)
	if err != nil {
		return "", "", err
	}
	revision, err = git(path, "rev-parse", "--verify", "refs/heads/"+r.Trunk)
	return revision, destination, err
}

// Push publishes child trunks in dependency order, then the workspace root.
// Each repository is resolved and pushed before inspecting the next checkout.
func (e *Engine) Push() error {
	repositories, err := e.pushRepositories()
	if err != nil {
		return err
	}
	completed := []string{}
	for index, repository := range repositories {
		r := repository.repository
		skipped, err := e.pushRepository(repository)
		if err != nil {
			pending := make([]string, 0, len(repositories)-index-1)
			for _, next := range repositories[index+1:] {
				pending = append(pending, next.repository.Name)
			}
			return &PublicationError{
				Err:       failure("external_command", r.Name, fmt.Errorf("repository %s push: %w", r.Name, err)),
				Completed: completed,
				Blocked:   r.Name,
				Pending:   pending,
			}
		}
		if !skipped {
			completed = append(completed, r.Name)
		}
	}
	return nil
}

func (e *Engine) pushRepository(repository canonicalRepository) (skipped bool, err error) {
	r := repository.repository
	local, destination, err := pushTarget(repository)
	if err != nil {
		return false, err
	}
	if destination == "" {
		e.logOperationOutcome("push", r.Name, repository.path, "", "skipped", LogWarning, ": no origin remote")
		return true, nil
	}
	changed, err := pushTrunk(repository.path, destination, local+":refs/heads/"+r.Trunk)
	if err != nil {
		return false, err
	}
	if _, err := git(repository.path, "update-ref", "refs/remotes/origin/"+r.Trunk, local); err != nil {
		return false, fmt.Errorf("trunk was published but cached origin/%s could not be updated: %w", r.Trunk, err)
	}
	if changed {
		e.logOperationOutcome("push", r.Name, repository.path, "", "pushed", LogChanged, " trunk %s at %s", r.Trunk, abbreviateRevision(local))
	} else {
		e.logOperationOutcome("push", r.Name, repository.path, fmt.Sprintf("trunk %s ", r.Trunk), "unchanged", LogSuccess, " at %s", abbreviateRevision(local))
	}
	return false, nil
}

func pushTrunk(path, destination, refspec string) (bool, error) {
	output, err := gitRaw(path, "push", "--porcelain", "--", destination, refspec)
	if err != nil {
		return false, err
	}
	for _, line := range strings.Split(strings.TrimRight(output, "\r\n"), "\n") {
		if len(line) < 2 || line[1] != '\t' {
			continue
		}
		switch line[0] {
		case '=':
			return false, nil
		case ' ', '*', '+', '-':
			return true, nil
		default:
			return false, fmt.Errorf("unexpected git push porcelain status %q", line[0])
		}
	}
	return false, fmt.Errorf("git push porcelain output did not include a ref status")
}

func pushDestination(path string) (string, error) {
	value, err := git(path, "remote", "get-url", "--push", "--all", "origin")
	if err != nil {
		return "", err
	}
	urls := strings.Split(value, "\n")
	if len(urls) != 1 || urls[0] == "" {
		return "", fmt.Errorf("publication requires exactly one origin push URL")
	}
	return urls[0], nil
}

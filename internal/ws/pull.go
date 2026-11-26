package ws

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
)

type PullOutcome string

const (
	PullCloned    PullOutcome = "cloned"
	PullUpdated   PullOutcome = "updated"
	PullUnchanged PullOutcome = "unchanged"
	PullFailed    PullOutcome = "failed"
)

type PullRepositoryResult struct {
	Name    string      `json:"name"`
	Path    string      `json:"path"`
	Trunk   string      `json:"trunk"`
	Outcome PullOutcome `json:"outcome"`
	Before  string      `json:"before,omitempty"`
	After   string      `json:"after,omitempty"`
	Error   string      `json:"error,omitempty"`
}

func (e *Engine) baselineUsers(repository Repository, path, local, remote string) ([]string, error) {
	if ancestor(path, remote, local) {
		return nil, nil
	}
	manifests, err := e.All()
	if err != nil {
		return nil, err
	}
	changeSet := map[string]struct{}{}
	for _, manifest := range manifests {
		if manifest.State == "dropped" {
			continue
		}
		for _, state := range manifest.Repositories {
			if state.Repository.Name != repository.Name || state.Base == "" {
				continue
			}
			if ancestor(path, state.Base, local) && !ancestor(path, state.Base, remote) {
				changeSet[workspaceSelector(manifest)] = struct{}{}
			}
		}
	}
	changes := make([]string, 0, len(changeSet))
	for tag := range changeSet {
		changes = append(changes, tag)
	}
	sort.Strings(changes)
	return changes, nil
}

// Pull clones missing children and updates existing canonical trunks.
// It reports every repository and returns the combined failures.
func (e *Engine) Pull() ([]PullRepositoryResult, error) {
	repositories, err := e.orderedCanonicalTrunks()
	if err != nil {
		return nil, err
	}

	results := make([]PullRepositoryResult, 0, len(repositories))
	var failures []error
	for _, item := range repositories {
		result := PullRepositoryResult{Name: item.repository.Name, Path: item.path, Trunk: item.repository.Trunk}
		if err := e.pullRepository(item, &result); err != nil {
			err = fmt.Errorf("repository %s pull %w", result.Name, err)
			result.Outcome, result.Error = PullFailed, err.Error()
			failures = append(failures, err)
		}
		results = append(results, result)
	}
	return results, errors.Join(failures...)
}

// pullCheckout distinguishes a missing child from a blocked checkout using only local state.
func (e *Engine) pullCheckout(item canonicalRepository) (missing bool, revision string, err error) {
	r := item.repository
	if err := e.reconcileSync(r, item.path); err != nil {
		return false, "", err
	}
	if r.Name == "root" {
		if err := validateCheckoutRoot(item.path, r.Name); err != nil {
			return false, "", err
		}
		if _, err := git(item.path, "remote", "get-url", "origin"); err != nil {
			return false, "", fmt.Errorf("resolve origin: %w", err)
		}
	} else {
		if _, err := os.Lstat(item.path); os.IsNotExist(err) {
			return true, "", nil
		} else if err != nil {
			return false, "", err
		}
		if err := validateOriginFrom(item.path, r, e.Root); err != nil {
			return false, "", err
		}
	}
	revision, err = localBaseline(&RepoState{Repository: r, Origin: item.path})
	return false, revision, err
}

func (e *Engine) pullRepository(item canonicalRepository, result *PullRepositoryResult) error {
	r := item.repository
	missing, local, err := e.pullCheckout(item)
	if err != nil {
		return fmt.Errorf("preflight: %w", err)
	}
	if missing {
		outcome, err := bootstrapRepository(e.Root, r, item.path)
		if err != nil {
			return fmt.Errorf("clone: %w", err)
		}
		if outcome == "cloned" {
			result.After, err = head(item.path)
			if err != nil {
				return fmt.Errorf("resolve cloned HEAD: %w", err)
			}
			result.Outcome = PullCloned
			return nil
		}

		// Another process created the checkout after preflight.
		_, local, err = e.pullCheckout(item)
		if err != nil {
			return fmt.Errorf("preflight: %w", err)
		}
		if local == "" {
			return fmt.Errorf("preflight: checkout disappeared after validation")
		}
	}
	result.Before = local

	remoteRef := "refs/remotes/origin/" + r.Trunk
	refspec := "+refs/heads/" + r.Trunk + ":" + remoteRef
	if _, err := git(item.path, "fetch", "--no-tags", "origin", refspec); err != nil {
		return fmt.Errorf("fetch remote trunk %s: %w", r.Trunk, err)
	}
	remote, err := git(item.path, "rev-parse", remoteRef)
	if err != nil {
		return fmt.Errorf("resolve fetched remote trunk %s: %w", r.Trunk, err)
	}
	users, err := e.baselineUsers(r, item.path, local, remote)
	if err != nil {
		return fmt.Errorf("inspect active workspace baselines: %w", err)
	}
	if len(users) > 0 {
		return fmt.Errorf("would rewrite commits recorded as baselines by active managed workspaces: %s; refresh, merge, or drop those workspaces before retrying", strings.Join(users, ", "))
	}
	if _, err := git(item.path, "rebase", remoteRef); err != nil {
		return fmt.Errorf("rebase interrupted: %w; resolve the conflicts and run git rebase --continue, or run git rebase --abort, in %s before retrying", err, item.path)
	}
	result.After, err = head(item.path)
	if err != nil {
		return fmt.Errorf("resolve updated HEAD: %w", err)
	}
	result.Outcome = PullUpdated
	if result.After == result.Before {
		result.Outcome = PullUnchanged
	}
	return nil
}

func (e *Engine) planPull(plan *OperationPlan) {
	repositories, err := e.orderedCanonicalTrunks()
	if err != nil {
		plan.Blockers = append(plan.Blockers, err.Error())
		return
	}
	for _, item := range repositories {
		r := item.repository
		step := PlanStep{
			Phase: "pull", Repository: r.Name, Target: item.path,
			Effect: "fetch and rebase local trunk " + r.Trunk, Checkpoint: "pending",
		}
		missing, _, err := e.pullCheckout(item)
		switch {
		case err != nil:
			step.Checkpoint = "blocked"
			step.Effect = err.Error()
			plan.Blockers = append(plan.Blockers, fmt.Sprintf("repository %s pull preflight: %s", r.Name, err))
		case missing:
			step.Phase = "clone"
			step.Effect = "clone configured origin at " + r.Trunk
		}
		plan.Steps = append(plan.Steps, step)
	}
}

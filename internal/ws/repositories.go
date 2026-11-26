package ws

import (
	"fmt"
	"path/filepath"
)

type canonicalRepository struct {
	repository Repository
	path       string
}

func (e *Engine) canonicalRepositories() ([]canonicalRepository, error) {
	rootURL, err := git(e.Root, "remote", "get-url", "origin")
	if err != nil {
		return nil, fmt.Errorf("repository root: resolve origin: %w", err)
	}
	root := canonicalRepository{
		repository: Repository{Name: "root", URL: rootURL, Trunk: e.Config.Root.Trunk},
		path:       e.Root,
	}
	repositories := make([]canonicalRepository, 0, len(e.Config.Children)+1)
	repositories = append(repositories, root)
	for _, repository := range e.Config.Children {
		repositories = append(repositories, canonicalRepository{
			repository: repository,
			path:       filepath.Join(e.Root, repository.Path),
		})
	}
	return repositories, nil
}

func (e *Engine) orderedCanonicalTrunks() ([]canonicalRepository, error) {
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

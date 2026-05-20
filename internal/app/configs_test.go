package app

import (
	"context"
	"errors"
	"testing"
)

var errMemoryConfigNotFound = errors.New("not found")

type memoryConfigStore struct {
	projects []Project
	configs  map[string]ProjectConfig
}

func (m *memoryConfigStore) FindProjectForOwner(_ context.Context, ownerID, projectID string) (Project, error) {
	for _, project := range m.projects {
		if project.ID == projectID && project.OwnerID == ownerID {
			return project, nil
		}
	}
	return Project{}, errMemoryConfigNotFound
}

func (m *memoryConfigStore) UpsertProjectConfig(_ context.Context, cfg ProjectConfig) (ProjectConfig, error) {
	if m.configs == nil {
		m.configs = map[string]ProjectConfig{}
	}
	m.configs[cfg.ProjectID+":"+cfg.Kind] = cfg
	return cfg, nil
}

func (m *memoryConfigStore) FindProjectConfig(_ context.Context, projectID, kind string) (ProjectConfig, error) {
	cfg, ok := m.configs[projectID+":"+kind]
	if !ok {
		return ProjectConfig{}, errMemoryConfigNotFound
	}
	return cfg, nil
}

func (m *memoryConfigStore) FindProjectConfigByCode(_ context.Context, code, kind string) (Project, ProjectConfig, error) {
	for _, project := range m.projects {
		if project.Code == code {
			cfg, ok := m.configs[project.ID+":"+kind]
			if !ok {
				return Project{}, ProjectConfig{}, errMemoryConfigNotFound
			}
			return project, cfg, nil
		}
	}
	return Project{}, ProjectConfig{}, errMemoryConfigNotFound
}

func (m *memoryConfigStore) FindProjectConfigByCodeForOwner(_ context.Context, ownerID, code, kind string) (Project, ProjectConfig, error) {
	for _, project := range m.projects {
		if project.OwnerID == ownerID && project.Code == code {
			cfg, ok := m.configs[project.ID+":"+kind]
			if !ok {
				return Project{}, ProjectConfig{}, errMemoryConfigNotFound
			}
			return project, cfg, nil
		}
	}
	return Project{}, ProjectConfig{}, errMemoryConfigNotFound
}

func TestGetByProjectCodeForOwnerIsolatesUsers(t *testing.T) {
	store := &memoryConfigStore{
		projects: []Project{
			{ID: "alice-project", OwnerID: "alice-id", Code: "shared-code", Name: "Alice Project"},
			{ID: "bob-project", OwnerID: "bob-id", Code: "bob-code", Name: "Bob Project"},
		},
		configs: map[string]ProjectConfig{
			"alice-project:config": {ProjectID: "alice-project", Kind: "config", Ciphertext: "alice-cipher"},
			"bob-project:config":   {ProjectID: "bob-project", Kind: "config", Ciphertext: "bob-cipher"},
		},
	}
	service := NewConfigService(store)

	project, cfg, err := service.GetByProjectCodeForOwner(context.Background(), User{ID: "alice-id"}, "shared-code", "config")
	if err != nil {
		t.Fatalf("alice should read own project: %v", err)
	}
	if project.ID != "alice-project" || cfg.Ciphertext != "alice-cipher" {
		t.Fatalf("unexpected alice config: project=%+v cfg=%+v", project, cfg)
	}
	if _, _, err := service.GetByProjectCodeForOwner(context.Background(), User{ID: "bob-id"}, "shared-code", "config"); err == nil {
		t.Fatal("bob must not read alice project with his own access token")
	}
}

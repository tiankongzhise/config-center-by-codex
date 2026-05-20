package app

import (
	"context"
	"errors"
)

type ConfigStore interface {
	FindProjectForOwner(ctx context.Context, ownerID, projectID string) (Project, error)
	UpsertProjectConfig(ctx context.Context, cfg ProjectConfig) (ProjectConfig, error)
	FindProjectConfig(ctx context.Context, projectID, kind string) (ProjectConfig, error)
	FindProjectConfigByCode(ctx context.Context, code, kind string) (Project, ProjectConfig, error)
	FindProjectConfigByCodeForOwner(ctx context.Context, ownerID, code, kind string) (Project, ProjectConfig, error)
}

type ConfigService struct {
	store ConfigStore
}

type SaveConfigInput struct {
	Kind      string `json:"kind"`
	Plaintext string `json:"plaintext"`
}

func NewConfigService(store ConfigStore) *ConfigService {
	return &ConfigService{store: store}
}

func (s *ConfigService) Save(ctx context.Context, owner User, projectID string, input SaveConfigInput) (ProjectConfig, error) {
	kind, err := normalizeConfigKind(input.Kind)
	if err != nil {
		return ProjectConfig{}, err
	}
	project, err := s.store.FindProjectForOwner(ctx, owner.ID, projectID)
	if err != nil {
		return ProjectConfig{}, err
	}
	if project.RSAPublicKey == "" {
		return ProjectConfig{}, errors.New("project rsa public key is required before saving config")
	}
	ciphertext, contentHash, err := EncryptWithPublicKey(project.RSAPublicKey, input.Plaintext)
	if err != nil {
		return ProjectConfig{}, err
	}
	return s.store.UpsertProjectConfig(ctx, ProjectConfig{
		ProjectID:   project.ID,
		Kind:        kind,
		Ciphertext:  ciphertext,
		ContentHash: contentHash,
	})
}

func (s *ConfigService) GetForOwner(ctx context.Context, owner User, projectID, kind string) (ProjectConfig, error) {
	kind, err := normalizeConfigKind(kind)
	if err != nil {
		return ProjectConfig{}, err
	}
	project, err := s.store.FindProjectForOwner(ctx, owner.ID, projectID)
	if err != nil {
		return ProjectConfig{}, err
	}
	return s.store.FindProjectConfig(ctx, project.ID, kind)
}

func (s *ConfigService) GetByProjectCode(ctx context.Context, code, kind string) (Project, ProjectConfig, error) {
	kind, err := normalizeConfigKind(kind)
	if err != nil {
		return Project{}, ProjectConfig{}, err
	}
	return s.store.FindProjectConfigByCode(ctx, code, kind)
}

func (s *ConfigService) GetByProjectCodeForOwner(ctx context.Context, owner User, code, kind string) (Project, ProjectConfig, error) {
	kind, err := normalizeConfigKind(kind)
	if err != nil {
		return Project{}, ProjectConfig{}, err
	}
	return s.store.FindProjectConfigByCodeForOwner(ctx, owner.ID, code, kind)
}

func normalizeConfigKind(kind string) (string, error) {
	switch kind {
	case "config", "env":
		return kind, nil
	default:
		return "", errors.New("config kind must be config or env")
	}
}

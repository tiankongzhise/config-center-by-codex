package app

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/tiankongzhise/config-center-by-codex/internal/ids"
)

var projectCodePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{2,62}$`)

type ProjectStore interface {
	CreateProject(ctx context.Context, project Project) (Project, error)
	ListProjects(ctx context.Context, ownerID string) ([]Project, error)
	FindProjectForOwner(ctx context.Context, ownerID, projectID string) (Project, error)
	UpdateProject(ctx context.Context, project Project) (Project, error)
	DeleteProject(ctx context.Context, ownerID, projectID string) error
}

type ProjectService struct {
	store ProjectStore
	newID func() string
}

type CreateProjectInput struct {
	Name         string
	Code         string
	Description  string
	RSAPublicKey string
}

type UpdateProjectInput struct {
	Name         string
	Description  string
	RSAPublicKey string
}

func NewProjectService(store ProjectStore) *ProjectService {
	return &ProjectService{store: store, newID: ids.New}
}

func (s *ProjectService) Create(ctx context.Context, owner User, input CreateProjectInput) (Project, error) {
	name, code, description, publicKey, err := normalizeProjectInput(input.Name, input.Code, input.Description, input.RSAPublicKey)
	if err != nil {
		return Project{}, err
	}
	return s.store.CreateProject(ctx, Project{
		ID:           s.newID(),
		OwnerID:      owner.ID,
		Name:         name,
		Code:         code,
		Description:  description,
		RSAPublicKey: publicKey,
	})
}

func (s *ProjectService) List(ctx context.Context, owner User) ([]Project, error) {
	return s.store.ListProjects(ctx, owner.ID)
}

func (s *ProjectService) Get(ctx context.Context, owner User, projectID string) (Project, error) {
	return s.store.FindProjectForOwner(ctx, owner.ID, projectID)
}

func (s *ProjectService) Update(ctx context.Context, owner User, projectID string, input UpdateProjectInput) (Project, error) {
	name, _, description, publicKey, err := normalizeProjectInput(input.Name, "placeholder", input.Description, input.RSAPublicKey)
	if err != nil {
		return Project{}, err
	}
	return s.store.UpdateProject(ctx, Project{
		ID:           projectID,
		OwnerID:      owner.ID,
		Name:         name,
		Description:  description,
		RSAPublicKey: publicKey,
	})
}

func (s *ProjectService) Delete(ctx context.Context, owner User, projectID string) error {
	return s.store.DeleteProject(ctx, owner.ID, projectID)
}

func normalizeProjectInput(name, code, description, publicKey string) (string, string, string, string, error) {
	name = strings.TrimSpace(name)
	code = strings.TrimSpace(code)
	description = strings.TrimSpace(description)
	publicKey = strings.TrimSpace(publicKey)
	if name == "" || len(name) > 80 {
		return "", "", "", "", errors.New("project name is required and must be at most 80 characters")
	}
	if !projectCodePattern.MatchString(code) {
		return "", "", "", "", errors.New("project code must start with lowercase letter and contain 3-63 lowercase letters, numbers, or hyphen")
	}
	if len(description) > 500 {
		return "", "", "", "", errors.New("project description must be at most 500 characters")
	}
	if publicKey != "" {
		if err := ValidateRSAPublicKey(publicKey); err != nil {
			return "", "", "", "", err
		}
	}
	return name, code, description, publicKey, nil
}

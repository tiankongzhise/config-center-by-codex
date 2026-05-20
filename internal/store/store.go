package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/lib/pq"
	"github.com/tiankongzhise/config-center-by-codex/internal/app"
)

var (
	ErrNotFound  = errors.New("not found")
	ErrConflict  = errors.New("conflict")
	ErrForbidden = errors.New("forbidden")
)

type Store struct {
	db *sql.DB
}

func Open(databaseURL string) (*Store, error) {
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)
	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

func (s *Store) CreateUser(ctx context.Context, user app.User) (app.User, error) {
	row := s.db.QueryRowContext(ctx, `
		INSERT INTO users (id, username, password_hash, display_name)
		VALUES ($1, $2, $3, $4)
		RETURNING id, username, password_hash, display_name, created_at, updated_at
	`, user.ID, user.Username, user.PasswordHash, user.DisplayName)
	created, err := scanUser(row)
	if isUniqueViolation(err) {
		return app.User{}, ErrConflict
	}
	return created, err
}

func (s *Store) FindUserByUsername(ctx context.Context, username string) (app.User, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, username, password_hash, display_name, created_at, updated_at
		FROM users
		WHERE lower(username) = lower($1)
	`, username)
	return scanUser(row)
}

func (s *Store) FindUserByID(ctx context.Context, id string) (app.User, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, username, password_hash, display_name, created_at, updated_at
		FROM users
		WHERE id = $1
	`, id)
	return scanUser(row)
}

func (s *Store) CreateSession(ctx context.Context, session app.Session) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO sessions (id, user_id, token_hash, expires_at)
		VALUES ($1, $2, $3, $4)
	`, session.ID, session.UserID, session.TokenHash, session.ExpiresAt)
	return err
}

func (s *Store) FindUserBySessionToken(ctx context.Context, tokenHash string, now time.Time) (app.User, app.Session, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT
			u.id, u.username, u.password_hash, u.display_name, u.created_at, u.updated_at,
			s.id, s.user_id, s.token_hash, s.expires_at, s.created_at, s.revoked_at
		FROM sessions s
		JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = $1
			AND s.revoked_at IS NULL
			AND s.expires_at > $2
	`, tokenHash, now)

	var user app.User
	var session app.Session
	if err := row.Scan(
		&user.ID, &user.Username, &user.PasswordHash, &user.DisplayName, &user.CreatedAt, &user.UpdatedAt,
		&session.ID, &session.UserID, &session.TokenHash, &session.ExpiresAt, &session.CreatedAt, &session.RevokedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return app.User{}, app.Session{}, ErrNotFound
		}
		return app.User{}, app.Session{}, err
	}
	return user, session, nil
}

func (s *Store) FindUserByAPIAccessToken(ctx context.Context, tokenHash string, now time.Time) (app.User, app.APIToken, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT
			u.id, u.username, u.password_hash, u.display_name, u.created_at, u.updated_at,
			t.id, t.user_id, t.access_token_hash, t.refresh_token_hash, t.access_token_expires_at,
			t.refresh_token_expires_at, t.created_at, t.revoked_at
		FROM api_tokens t
		JOIN users u ON u.id = t.user_id
		WHERE t.access_token_hash = $1
			AND t.revoked_at IS NULL
			AND t.access_token_expires_at > $2
	`, tokenHash, now)
	return scanUserAndAPIToken(row)
}

func (s *Store) FindUserByAPIRefreshToken(ctx context.Context, tokenHash string, now time.Time) (app.User, app.APIToken, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT
			u.id, u.username, u.password_hash, u.display_name, u.created_at, u.updated_at,
			t.id, t.user_id, t.access_token_hash, t.refresh_token_hash, t.access_token_expires_at,
			t.refresh_token_expires_at, t.created_at, t.revoked_at
		FROM api_tokens t
		JOIN users u ON u.id = t.user_id
		WHERE t.refresh_token_hash = $1
			AND t.revoked_at IS NULL
			AND t.refresh_token_expires_at > $2
	`, tokenHash, now)
	return scanUserAndAPIToken(row)
}

func (s *Store) RevokeSession(ctx context.Context, tokenHash string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE sessions SET revoked_at = now()
		WHERE token_hash = $1 AND revoked_at IS NULL
	`, tokenHash)
	return err
}

func (s *Store) CreateAPIToken(ctx context.Context, token app.APIToken) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO api_tokens (
			id, user_id, access_token_hash, refresh_token_hash,
			access_token_expires_at, refresh_token_expires_at
		)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, token.ID, token.UserID, token.AccessTokenHash, token.RefreshTokenHash, token.AccessTokenExpiresAt, token.RefreshTokenExpiresAt)
	return err
}

func (s *Store) RevokeAPIToken(ctx context.Context, tokenID string) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE api_tokens SET revoked_at = now()
		WHERE id = $1 AND revoked_at IS NULL
	`, tokenID)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) CreateProject(ctx context.Context, project app.Project) (app.Project, error) {
	row := s.db.QueryRowContext(ctx, `
		INSERT INTO projects (id, owner_id, name, code, description, rsa_public_key)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, owner_id, name, code, description, rsa_public_key, created_at, updated_at
	`, project.ID, project.OwnerID, project.Name, project.Code, project.Description, project.RSAPublicKey)
	created, err := scanProject(row)
	if isUniqueViolation(err) {
		return app.Project{}, ErrConflict
	}
	return created, err
}

func (s *Store) ListProjects(ctx context.Context, ownerID string) ([]app.Project, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, owner_id, name, code, description, rsa_public_key, created_at, updated_at
		FROM projects
		WHERE owner_id = $1
		ORDER BY created_at DESC
	`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var projects []app.Project
	for rows.Next() {
		project, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		projects = append(projects, project)
	}
	return projects, rows.Err()
}

func (s *Store) FindProjectForOwner(ctx context.Context, ownerID, projectID string) (app.Project, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, owner_id, name, code, description, rsa_public_key, created_at, updated_at
		FROM projects
		WHERE id = $1 AND owner_id = $2
	`, projectID, ownerID)
	return scanProject(row)
}

func (s *Store) FindProjectByCode(ctx context.Context, code string) (app.Project, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, owner_id, name, code, description, rsa_public_key, created_at, updated_at
		FROM projects
		WHERE code = $1
	`, code)
	return scanProject(row)
}

func (s *Store) UpdateProject(ctx context.Context, project app.Project) (app.Project, error) {
	result, err := s.db.ExecContext(ctx, `
		UPDATE projects
		SET name = $1, description = $2, rsa_public_key = $3, updated_at = now()
		WHERE id = $4 AND owner_id = $5
	`, project.Name, project.Description, project.RSAPublicKey, project.ID, project.OwnerID)
	if err != nil {
		return app.Project{}, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return app.Project{}, err
	}
	if count == 0 {
		return app.Project{}, ErrNotFound
	}
	return s.FindProjectForOwner(ctx, project.OwnerID, project.ID)
}

func (s *Store) DeleteProject(ctx context.Context, ownerID, projectID string) error {
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM projects
		WHERE id = $1 AND owner_id = $2
	`, projectID, ownerID)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) UpsertProjectConfig(ctx context.Context, cfg app.ProjectConfig) (app.ProjectConfig, error) {
	row := s.db.QueryRowContext(ctx, `
		INSERT INTO project_configs (project_id, kind, ciphertext, content_hash)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (project_id, kind)
		DO UPDATE SET ciphertext = EXCLUDED.ciphertext, content_hash = EXCLUDED.content_hash, updated_at = now()
		RETURNING project_id, kind, ciphertext, content_hash, updated_at
	`, cfg.ProjectID, cfg.Kind, cfg.Ciphertext, cfg.ContentHash)
	return scanProjectConfig(row)
}

func (s *Store) FindProjectConfig(ctx context.Context, projectID, kind string) (app.ProjectConfig, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT project_id, kind, ciphertext, content_hash, updated_at
		FROM project_configs
		WHERE project_id = $1 AND kind = $2
	`, projectID, kind)
	return scanProjectConfig(row)
}

func (s *Store) FindProjectConfigByCode(ctx context.Context, code, kind string) (app.Project, app.ProjectConfig, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT
			p.id, p.owner_id, p.name, p.code, p.description, p.rsa_public_key, p.created_at, p.updated_at,
			c.project_id, c.kind, c.ciphertext, c.content_hash, c.updated_at
		FROM projects p
		JOIN project_configs c ON c.project_id = p.id
		WHERE p.code = $1 AND c.kind = $2
	`, code, kind)

	var project app.Project
	var cfg app.ProjectConfig
	if err := row.Scan(
		&project.ID, &project.OwnerID, &project.Name, &project.Code, &project.Description, &project.RSAPublicKey, &project.CreatedAt, &project.UpdatedAt,
		&cfg.ProjectID, &cfg.Kind, &cfg.Ciphertext, &cfg.ContentHash, &cfg.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return app.Project{}, app.ProjectConfig{}, ErrNotFound
		}
		return app.Project{}, app.ProjectConfig{}, err
	}
	return project, cfg, nil
}

func (s *Store) FindProjectConfigByCodeForOwner(ctx context.Context, ownerID, code, kind string) (app.Project, app.ProjectConfig, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT
			p.id, p.owner_id, p.name, p.code, p.description, p.rsa_public_key, p.created_at, p.updated_at,
			c.project_id, c.kind, c.ciphertext, c.content_hash, c.updated_at
		FROM projects p
		JOIN project_configs c ON c.project_id = p.id
		WHERE p.owner_id = $1 AND p.code = $2 AND c.kind = $3
	`, ownerID, code, kind)

	var project app.Project
	var cfg app.ProjectConfig
	if err := row.Scan(
		&project.ID, &project.OwnerID, &project.Name, &project.Code, &project.Description, &project.RSAPublicKey, &project.CreatedAt, &project.UpdatedAt,
		&cfg.ProjectID, &cfg.Kind, &cfg.Ciphertext, &cfg.ContentHash, &cfg.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return app.Project{}, app.ProjectConfig{}, ErrNotFound
		}
		return app.Project{}, app.ProjectConfig{}, err
	}
	return project, cfg, nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanUser(row scanner) (app.User, error) {
	var user app.User
	if err := row.Scan(&user.ID, &user.Username, &user.PasswordHash, &user.DisplayName, &user.CreatedAt, &user.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return app.User{}, ErrNotFound
		}
		if isUniqueViolation(err) {
			return app.User{}, ErrConflict
		}
		return app.User{}, err
	}
	return user, nil
}

func scanProject(row scanner) (app.Project, error) {
	var project app.Project
	if err := row.Scan(
		&project.ID,
		&project.OwnerID,
		&project.Name,
		&project.Code,
		&project.Description,
		&project.RSAPublicKey,
		&project.CreatedAt,
		&project.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return app.Project{}, ErrNotFound
		}
		if isUniqueViolation(err) {
			return app.Project{}, ErrConflict
		}
		return app.Project{}, err
	}
	return project, nil
}

func scanProjectConfig(row scanner) (app.ProjectConfig, error) {
	var cfg app.ProjectConfig
	if err := row.Scan(&cfg.ProjectID, &cfg.Kind, &cfg.Ciphertext, &cfg.ContentHash, &cfg.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return app.ProjectConfig{}, ErrNotFound
		}
		return app.ProjectConfig{}, err
	}
	return cfg, nil
}

func scanUserAndAPIToken(row scanner) (app.User, app.APIToken, error) {
	var user app.User
	var token app.APIToken
	if err := row.Scan(
		&user.ID, &user.Username, &user.PasswordHash, &user.DisplayName, &user.CreatedAt, &user.UpdatedAt,
		&token.ID, &token.UserID, &token.AccessTokenHash, &token.RefreshTokenHash,
		&token.AccessTokenExpiresAt, &token.RefreshTokenExpiresAt, &token.CreatedAt, &token.RevokedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return app.User{}, app.APIToken{}, ErrNotFound
		}
		return app.User{}, app.APIToken{}, err
	}
	return user, token, nil
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	var pqErr *pq.Error
	if errors.As(err, &pqErr) {
		return pqErr.Code == "23505"
	}
	return strings.Contains(err.Error(), "duplicate key")
}

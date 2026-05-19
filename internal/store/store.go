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

func (s *Store) RevokeSession(ctx context.Context, tokenHash string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE sessions SET revoked_at = now()
		WHERE token_hash = $1 AND revoked_at IS NULL
	`, tokenHash)
	return err
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

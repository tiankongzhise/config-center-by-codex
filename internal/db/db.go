package db

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	_ "github.com/lib/pq"
	"github.com/tiankongzhise/config-center-by-codex/internal/config"
)

type DatabaseConfig struct {
	ConfigDBName     string
	ConfigDBUser     string
	ConfigDBPassword string
}

func EnsureDatabaseAndUser(ctx context.Context, cfg config.Config) (DatabaseConfig, error) {
	if cfg.PostgresAdmin == "" || cfg.PostgresAdminSecret == "" {
		return DatabaseConfig{}, errors.New("PG_ADMIN and PG_ADMIN_SECRET are required")
	}
	if cfg.ConfigDBPassword == "" {
		password, err := randomPassword()
		if err != nil {
			return DatabaseConfig{}, err
		}
		cfg.ConfigDBPassword = password
	}

	adminDB, err := sql.Open("postgres", postgresURL(cfg.PostgresHost, cfg.PostgresPort, "postgres", cfg.PostgresAdmin, cfg.PostgresAdminSecret))
	if err != nil {
		return DatabaseConfig{}, err
	}
	defer adminDB.Close()
	adminDB.SetConnMaxLifetime(5 * time.Minute)
	if err := adminDB.PingContext(ctx); err != nil {
		return DatabaseConfig{}, err
	}

	if err := ensureRole(ctx, adminDB, cfg.ConfigDBUser, cfg.ConfigDBPassword); err != nil {
		return DatabaseConfig{}, err
	}
	if err := ensureDatabase(ctx, adminDB, cfg.ConfigDBName, cfg.ConfigDBUser); err != nil {
		return DatabaseConfig{}, err
	}

	targetDB, err := sql.Open("postgres", postgresURL(cfg.PostgresHost, cfg.PostgresPort, cfg.ConfigDBName, cfg.PostgresAdmin, cfg.PostgresAdminSecret))
	if err != nil {
		return DatabaseConfig{}, err
	}
	defer targetDB.Close()
	if err := targetDB.PingContext(ctx); err != nil {
		return DatabaseConfig{}, err
	}
	if err := ensurePrivileges(ctx, targetDB, cfg.ConfigDBName, cfg.ConfigDBUser); err != nil {
		return DatabaseConfig{}, err
	}

	return DatabaseConfig{
		ConfigDBName:     cfg.ConfigDBName,
		ConfigDBUser:     cfg.ConfigDBUser,
		ConfigDBPassword: cfg.ConfigDBPassword,
	}, nil
}

func Migrate(ctx context.Context, databaseURL string) error {
	if databaseURL == "" {
		return errors.New("CONFIG_CENTER_DATABASE_URL or CONFIG_CENTER_DB_PASSWORD is required")
	}

	conn, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := conn.PingContext(ctx); err != nil {
		return err
	}

	for _, statement := range migrationStatements {
		if _, err := conn.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

func ensureRole(ctx context.Context, conn *sql.DB, username, password string) error {
	var exists bool
	if err := conn.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = $1)", username).Scan(&exists); err != nil {
		return err
	}
	if exists {
		_, err := conn.ExecContext(ctx, "ALTER ROLE "+quoteIdent(username)+" WITH LOGIN PASSWORD $1", password)
		return err
	}
	_, err := conn.ExecContext(ctx, "CREATE ROLE "+quoteIdent(username)+" WITH LOGIN PASSWORD $1", password)
	return err
}

func ensureDatabase(ctx context.Context, conn *sql.DB, dbName, owner string) error {
	var exists bool
	if err := conn.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)", dbName).Scan(&exists); err != nil {
		return err
	}
	if exists {
		_, err := conn.ExecContext(ctx, "ALTER DATABASE "+quoteIdent(dbName)+" OWNER TO "+quoteIdent(owner))
		return err
	}
	_, err := conn.ExecContext(ctx, "CREATE DATABASE "+quoteIdent(dbName)+" OWNER "+quoteIdent(owner))
	return err
}

func ensurePrivileges(ctx context.Context, conn *sql.DB, dbName, username string) error {
	statements := []string{
		"GRANT CONNECT ON DATABASE " + quoteIdent(dbName) + " TO " + quoteIdent(username),
		"ALTER SCHEMA public OWNER TO " + quoteIdent(username),
		"GRANT ALL ON SCHEMA public TO " + quoteIdent(username),
	}
	for _, statement := range statements {
		if _, err := conn.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

func postgresURL(host, port, database, username, password string) string {
	u := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(username, password),
		Host:   host + ":" + port,
		Path:   "/" + database,
	}
	query := u.Query()
	query.Set("sslmode", "disable")
	u.RawQuery = query.Encode()
	return u.String()
}

func quoteIdent(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func randomPassword() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate database password: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

var migrationStatements = []string{
	`CREATE TABLE IF NOT EXISTS users (
		id TEXT PRIMARY KEY,
		username TEXT NOT NULL UNIQUE,
		password_hash TEXT NOT NULL,
		display_name TEXT NOT NULL DEFAULT '',
		created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`,
	`CREATE UNIQUE INDEX IF NOT EXISTS idx_users_username_lower ON users (lower(username))`,
	`CREATE TABLE IF NOT EXISTS sessions (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		token_hash TEXT NOT NULL UNIQUE,
		expires_at TIMESTAMPTZ NOT NULL,
		created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
		revoked_at TIMESTAMPTZ
	)`,
	`CREATE INDEX IF NOT EXISTS idx_sessions_user_id ON sessions(user_id)`,
	`CREATE INDEX IF NOT EXISTS idx_sessions_token_hash ON sessions(token_hash)`,
	`CREATE TABLE IF NOT EXISTS projects (
		id TEXT PRIMARY KEY,
		owner_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		name TEXT NOT NULL,
		code TEXT NOT NULL UNIQUE,
		description TEXT NOT NULL DEFAULT '',
		rsa_public_key TEXT NOT NULL DEFAULT '',
		created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`,
	`CREATE INDEX IF NOT EXISTS idx_projects_owner_id ON projects(owner_id)`,
	`CREATE TABLE IF NOT EXISTS project_configs (
		project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
		kind TEXT NOT NULL CHECK (kind IN ('config', 'env')),
		ciphertext TEXT NOT NULL,
		content_hash TEXT NOT NULL,
		updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
		PRIMARY KEY (project_id, kind)
	)`,
}

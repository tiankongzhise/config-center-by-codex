package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/tiankongzhise/config-center-by-codex/internal/ids"
)

var ErrInvalidCredentials = errors.New("invalid username or password")

type UserStore interface {
	CreateUser(ctx context.Context, user User) (User, error)
	FindUserByUsername(ctx context.Context, username string) (User, error)
	FindUserBySessionToken(ctx context.Context, tokenHash string, now time.Time) (User, Session, error)
	CreateSession(ctx context.Context, session Session) error
	RevokeSession(ctx context.Context, tokenHash string) error
}

type Service struct {
	store        UserStore
	sessionTTL   time.Duration
	now          func() time.Time
	newID        func() string
	newTokenFunc func() (string, error)
}

func NewService(store UserStore) *Service {
	return &Service{
		store:        store,
		sessionTTL:   24 * time.Hour,
		now:          time.Now,
		newID:        ids.New,
		newTokenFunc: NewToken,
	}
}

type RegisterInput struct {
	Username    string
	Password    string
	DisplayName string
}

type LoginResult struct {
	User      User
	Token     string
	ExpiresAt time.Time
}

func (s *Service) Register(ctx context.Context, input RegisterInput) (User, error) {
	username := NormalizeUsername(input.Username)
	if err := ValidateUsername(username); err != nil {
		return User{}, err
	}
	hash, err := HashPassword(input.Password)
	if err != nil {
		return User{}, err
	}
	displayName := strings.TrimSpace(input.DisplayName)
	if displayName == "" {
		displayName = username
	}
	return s.store.CreateUser(ctx, User{
		ID:           s.newID(),
		Username:     username,
		PasswordHash: hash,
		DisplayName:  displayName,
	})
}

func (s *Service) Login(ctx context.Context, username, password string) (LoginResult, error) {
	user, err := s.store.FindUserByUsername(ctx, NormalizeUsername(username))
	if err != nil {
		return LoginResult{}, ErrInvalidCredentials
	}
	if !CheckPassword(user.PasswordHash, password) {
		return LoginResult{}, ErrInvalidCredentials
	}

	token, err := s.newTokenFunc()
	if err != nil {
		return LoginResult{}, err
	}
	expiresAt := s.now().UTC().Add(s.sessionTTL)
	session := Session{
		ID:        s.newID(),
		UserID:    user.ID,
		TokenHash: HashToken(token),
		ExpiresAt: expiresAt,
	}
	if err := s.store.CreateSession(ctx, session); err != nil {
		return LoginResult{}, err
	}
	return LoginResult{User: user, Token: token, ExpiresAt: expiresAt}, nil
}

func (s *Service) CurrentUser(ctx context.Context, token string) (User, error) {
	if token == "" {
		return User{}, ErrInvalidCredentials
	}
	user, _, err := s.store.FindUserBySessionToken(ctx, HashToken(token), s.now().UTC())
	if err != nil {
		return User{}, ErrInvalidCredentials
	}
	return user, nil
}

func (s *Service) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	return s.store.RevokeSession(ctx, HashToken(token))
}

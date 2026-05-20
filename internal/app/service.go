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
	FindUserByAPIAccessToken(ctx context.Context, tokenHash string, now time.Time) (User, APIToken, error)
	FindUserByAPIRefreshToken(ctx context.Context, tokenHash string, now time.Time) (User, APIToken, error)
	CreateSession(ctx context.Context, session Session) error
	CreateAPIToken(ctx context.Context, token APIToken) error
	RevokeSession(ctx context.Context, tokenHash string) error
	RevokeAPIToken(ctx context.Context, tokenID string) error
}

type Service struct {
	store         UserStore
	sessionTTL    time.Duration
	apiAccessTTL  time.Duration
	apiRefreshTTL time.Duration
	now           func() time.Time
	newID         func() string
	newTokenFunc  func() (string, error)
}

func NewService(store UserStore) *Service {
	return &Service{
		store:         store,
		sessionTTL:    24 * time.Hour,
		apiAccessTTL:  30 * time.Minute,
		apiRefreshTTL: 7 * 24 * time.Hour,
		now:           time.Now,
		newID:         ids.New,
		newTokenFunc:  NewToken,
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

type APITokenResult struct {
	User                  User      `json:"user"`
	TokenType             string    `json:"tokenType"`
	AccessToken           string    `json:"accessToken"`
	AccessTokenExpiresAt  time.Time `json:"accessTokenExpiresAt"`
	RefreshToken          string    `json:"refreshToken"`
	RefreshTokenExpiresAt time.Time `json:"refreshTokenExpiresAt"`
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

func (s *Service) IssueAPITokenForCredentials(ctx context.Context, username, password string) (APITokenResult, error) {
	user, err := s.store.FindUserByUsername(ctx, NormalizeUsername(username))
	if err != nil {
		return APITokenResult{}, ErrInvalidCredentials
	}
	if !CheckPassword(user.PasswordHash, password) {
		return APITokenResult{}, ErrInvalidCredentials
	}
	return s.issueAPIToken(ctx, user)
}

func (s *Service) IssueAPITokenForUser(ctx context.Context, user User) (APITokenResult, error) {
	return s.issueAPIToken(ctx, user)
}

func (s *Service) RefreshAPIToken(ctx context.Context, refreshToken string) (APITokenResult, error) {
	if refreshToken == "" {
		return APITokenResult{}, ErrInvalidCredentials
	}
	user, oldToken, err := s.store.FindUserByAPIRefreshToken(ctx, HashToken(refreshToken), s.now().UTC())
	if err != nil {
		return APITokenResult{}, ErrInvalidCredentials
	}
	if err := s.store.RevokeAPIToken(ctx, oldToken.ID); err != nil {
		return APITokenResult{}, err
	}
	return s.issueAPIToken(ctx, user)
}

func (s *Service) CurrentAPIUser(ctx context.Context, accessToken string) (User, APIToken, error) {
	if accessToken == "" {
		return User{}, APIToken{}, ErrInvalidCredentials
	}
	user, token, err := s.store.FindUserByAPIAccessToken(ctx, HashToken(accessToken), s.now().UTC())
	if err != nil {
		return User{}, APIToken{}, ErrInvalidCredentials
	}
	return user, token, nil
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

func (s *Service) issueAPIToken(ctx context.Context, user User) (APITokenResult, error) {
	accessToken, err := s.newTokenFunc()
	if err != nil {
		return APITokenResult{}, err
	}
	refreshToken, err := s.newTokenFunc()
	if err != nil {
		return APITokenResult{}, err
	}
	now := s.now().UTC()
	result := APITokenResult{
		User:                  user,
		TokenType:             "Bearer",
		AccessToken:           accessToken,
		AccessTokenExpiresAt:  now.Add(s.apiAccessTTL),
		RefreshToken:          refreshToken,
		RefreshTokenExpiresAt: now.Add(s.apiRefreshTTL),
	}
	record := APIToken{
		ID:                    s.newID(),
		UserID:                user.ID,
		AccessTokenHash:       HashToken(accessToken),
		RefreshTokenHash:      HashToken(refreshToken),
		AccessTokenExpiresAt:  result.AccessTokenExpiresAt,
		RefreshTokenExpiresAt: result.RefreshTokenExpiresAt,
	}
	if err := s.store.CreateAPIToken(ctx, record); err != nil {
		return APITokenResult{}, err
	}
	return result, nil
}

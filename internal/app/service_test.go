package app

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"
)

var errMemoryStoreNotFound = errors.New("not found")

type memoryUserStore struct {
	usersByID       map[string]User
	usersByUsername map[string]User
	sessions        map[string]Session
	apiTokens       map[string]APIToken
	now             time.Time
}

func newMemoryUserStore(now time.Time) *memoryUserStore {
	return &memoryUserStore{
		usersByID:       map[string]User{},
		usersByUsername: map[string]User{},
		sessions:        map[string]Session{},
		apiTokens:       map[string]APIToken{},
		now:             now,
	}
}

func (m *memoryUserStore) CreateUser(_ context.Context, user User) (User, error) {
	m.usersByID[user.ID] = user
	m.usersByUsername[strings.ToLower(user.Username)] = user
	return user, nil
}

func (m *memoryUserStore) FindUserByUsername(_ context.Context, username string) (User, error) {
	user, ok := m.usersByUsername[strings.ToLower(username)]
	if !ok {
		return User{}, errMemoryStoreNotFound
	}
	return user, nil
}

func (m *memoryUserStore) FindUserBySessionToken(_ context.Context, tokenHash string, now time.Time) (User, Session, error) {
	for _, session := range m.sessions {
		if session.TokenHash == tokenHash && session.RevokedAt == nil && session.ExpiresAt.After(now) {
			user, ok := m.usersByID[session.UserID]
			if ok {
				return user, session, nil
			}
		}
	}
	return User{}, Session{}, errMemoryStoreNotFound
}

func (m *memoryUserStore) FindUserByAPIAccessToken(_ context.Context, tokenHash string, now time.Time) (User, APIToken, error) {
	for _, token := range m.apiTokens {
		if token.AccessTokenHash == tokenHash && token.RevokedAt == nil && token.AccessTokenExpiresAt.After(now) {
			user, ok := m.usersByID[token.UserID]
			if ok {
				return user, token, nil
			}
		}
	}
	return User{}, APIToken{}, errMemoryStoreNotFound
}

func (m *memoryUserStore) FindUserByAPIRefreshToken(_ context.Context, tokenHash string, now time.Time) (User, APIToken, error) {
	for _, token := range m.apiTokens {
		if token.RefreshTokenHash == tokenHash && token.RevokedAt == nil && token.RefreshTokenExpiresAt.After(now) {
			user, ok := m.usersByID[token.UserID]
			if ok {
				return user, token, nil
			}
		}
	}
	return User{}, APIToken{}, errMemoryStoreNotFound
}

func (m *memoryUserStore) CreateSession(_ context.Context, session Session) error {
	m.sessions[session.ID] = session
	return nil
}

func (m *memoryUserStore) CreateAPIToken(_ context.Context, token APIToken) error {
	m.apiTokens[token.ID] = token
	return nil
}

func (m *memoryUserStore) RevokeSession(_ context.Context, tokenHash string) error {
	for id, session := range m.sessions {
		if session.TokenHash == tokenHash {
			now := m.now
			session.RevokedAt = &now
			m.sessions[id] = session
		}
	}
	return nil
}

func (m *memoryUserStore) RevokeAPIToken(_ context.Context, tokenID string) error {
	token, ok := m.apiTokens[tokenID]
	if !ok || token.RevokedAt != nil {
		return errMemoryStoreNotFound
	}
	now := m.now
	token.RevokedAt = &now
	m.apiTokens[tokenID] = token
	return nil
}

func TestAPITokenRefreshOnlyRevokesCurrentTokenRecord(t *testing.T) {
	now := time.Date(2026, 5, 20, 8, 0, 0, 0, time.UTC)
	store := newMemoryUserStore(now)
	aliceHash, err := HashPassword("Strong-Password1!")
	if err != nil {
		t.Fatalf("hash alice password: %v", err)
	}
	bobHash, err := HashPassword("Another-Password1!")
	if err != nil {
		t.Fatalf("hash bob password: %v", err)
	}
	store.usersByID["alice-id"] = User{ID: "alice-id", Username: "alice", PasswordHash: aliceHash, DisplayName: "Alice"}
	store.usersByUsername["alice"] = store.usersByID["alice-id"]
	store.usersByID["bob-id"] = User{ID: "bob-id", Username: "bob", PasswordHash: bobHash, DisplayName: "Bob"}
	store.usersByUsername["bob"] = store.usersByID["bob-id"]

	tokens := []string{
		"alice-access-1", "alice-refresh-1",
		"alice-access-2", "alice-refresh-2",
		"bob-access-1", "bob-refresh-1",
		"alice-access-3", "alice-refresh-3",
		"bob-access-2", "bob-refresh-2",
	}
	nextToken := 0
	nextID := 0
	service := NewService(store)
	service.now = func() time.Time { return now }
	service.newID = func() string {
		nextID++
		return "token-id-" + strconv.Itoa(nextID)
	}
	service.newTokenFunc = func() (string, error) {
		if nextToken >= len(tokens) {
			t.Fatal("test token sequence exhausted")
		}
		token := tokens[nextToken]
		nextToken++
		return token, nil
	}

	aliceFirst, err := service.IssueAPITokenForCredentials(context.Background(), "alice", "Strong-Password1!")
	if err != nil {
		t.Fatalf("issue alice first token: %v", err)
	}
	aliceSecond, err := service.IssueAPITokenForCredentials(context.Background(), "alice", "Strong-Password1!")
	if err != nil {
		t.Fatalf("issue alice second token: %v", err)
	}
	bobFirst, err := service.IssueAPITokenForCredentials(context.Background(), "bob", "Another-Password1!")
	if err != nil {
		t.Fatalf("issue bob token: %v", err)
	}

	if _, _, err := service.CurrentAPIUser(context.Background(), aliceSecond.AccessToken); err != nil {
		t.Fatalf("expected alice second token to be valid before refresh: %v", err)
	}
	refreshedAlice, err := service.RefreshAPIToken(context.Background(), aliceFirst.RefreshToken)
	if err != nil {
		t.Fatalf("refresh alice first token: %v", err)
	}
	if refreshedAlice.User.ID != "alice-id" {
		t.Fatalf("expected refreshed token to stay scoped to alice, got %q", refreshedAlice.User.ID)
	}
	if _, _, err := service.CurrentAPIUser(context.Background(), aliceFirst.AccessToken); err == nil {
		t.Fatal("expected old alice access token from refreshed record to be revoked")
	}
	if _, err := service.RefreshAPIToken(context.Background(), aliceFirst.RefreshToken); err == nil {
		t.Fatal("expected old alice refresh token to be single-use")
	}
	if user, _, err := service.CurrentAPIUser(context.Background(), aliceSecond.AccessToken); err != nil || user.ID != "alice-id" {
		t.Fatalf("alice second token should remain valid, user=%+v err=%v", user, err)
	}
	if user, _, err := service.CurrentAPIUser(context.Background(), bobFirst.AccessToken); err != nil || user.ID != "bob-id" {
		t.Fatalf("bob token should remain valid, user=%+v err=%v", user, err)
	}
	refreshedBob, err := service.RefreshAPIToken(context.Background(), bobFirst.RefreshToken)
	if err != nil {
		t.Fatalf("bob refresh should not be affected by alice refresh: %v", err)
	}
	if refreshedBob.User.ID != "bob-id" {
		t.Fatalf("expected refreshed token to stay scoped to bob, got %q", refreshedBob.User.ID)
	}
}

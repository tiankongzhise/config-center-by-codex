package authlimit

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tiankongzhise/config-center-by-codex/internal/config"
)

func TestSignM2M(t *testing.T) {
	signature := SignM2M("secret", "123", map[string]any{
		"b": "2",
		"a": "1",
		"c": []string{"ignored"},
	})
	if signature == "" {
		t.Fatal("expected signature")
	}
	again := SignM2M("secret", "123", map[string]any{"a": "1", "b": "2"})
	if signature != again {
		t.Fatal("signature should sort keys and ignore complex values")
	}
}

func TestRandomPasswordMatchesAuthLimitRules(t *testing.T) {
	password, err := randomPassword()
	if err != nil {
		t.Fatalf("randomPassword() error = %v", err)
	}
	if !validAuthLimitPassword(password) {
		t.Fatalf("generated password does not match auth-limit rules: %q", password)
	}
}

func TestValidateOperatorCredentials(t *testing.T) {
	if err := validateOperatorCredentials("cfgcenter_ops", "Aa1!2345"); err != nil {
		t.Fatalf("expected valid credentials: %v", err)
	}
	if err := validateOperatorCredentials("config_center_operator", "Aa1!2345"); err == nil {
		t.Fatal("expected legacy long username to be rejected")
	}
	if err := validateOperatorCredentials("cfgcenter_ops", "Aa1!234567890123456789"); err == nil {
		t.Fatal("expected long password to be rejected")
	}
}

func TestLoginParsesTokenResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/auth/login" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		var payload struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode login payload: %v", err)
		}
		if payload.Username != "alice" || payload.Password != "secret" {
			t.Fatalf("unexpected payload: %+v", payload)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"tokenType":"Bearer","accessToken":"access-1","accessTokenExpiresAt":"2026-05-19T08:30:00Z","refreshToken":"refresh-1","refreshTokenExpiresAt":"2026-05-20T08:00:00Z"}}`))
	}))
	defer server.Close()

	client := New(config.Config{AuthLimitBaseURL: server.URL})
	result, err := client.Login(context.Background(), "alice", "secret")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if result.TokenType != "Bearer" || result.AccessToken != "access-1" {
		t.Fatalf("unexpected login result: %+v", result)
	}
	if result.RefreshToken != "refresh-1" || result.AccessTokenExpiresAt == "" {
		t.Fatalf("expected refresh token and expiry: %+v", result)
	}
}

func TestRegisterServiceUsesExistingConfigWithoutRemoteCalls(t *testing.T) {
	client := New(config.Config{AuthLimitBaseURL: "http://127.0.0.1:1"})
	registered, err := client.RegisterService(context.Background(), "token", config.Config{
		AuthLimitServiceID: "service-1",
		AuthLimitAppID:     "app-1",
		AuthLimitAppSecret: "secret-1",
	})
	if err != nil {
		t.Fatalf("register service: %v", err)
	}
	if registered.ServiceID != "service-1" || registered.AppID != "app-1" || registered.AppSecret != "secret-1" {
		t.Fatalf("unexpected registration result: %+v", registered)
	}
}

func TestRegisterServiceRejectsPartialAppCredentials(t *testing.T) {
	client := New(config.Config{AuthLimitBaseURL: "http://127.0.0.1:1"})
	_, err := client.RegisterService(context.Background(), "token", config.Config{
		AuthLimitServiceID: "service-1",
		AuthLimitAppID:     "app-1",
	})
	if err == nil {
		t.Fatal("expected partial app credentials to fail")
	}
}

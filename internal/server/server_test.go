package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tiankongzhise/config-center-by-codex/internal/config"
)

func TestHealthEndpoint(t *testing.T) {
	srv := New(config.Config{BaseURL: "http://localhost:8080"}, nil)
	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	response := httptest.NewRecorder()

	srv.Routes().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", response.Code)
	}
	if response.Body.String() == "" {
		t.Fatal("expected response body")
	}
}

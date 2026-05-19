package authlimit

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/tiankongzhise/config-center-by-codex/internal/config"
)

type Client struct {
	baseURL    string
	httpClient *http.Client
}

type AuthResult struct {
	Allowed     bool
	UserID      string
	AppID       string
	Permissions []string
	Roles       []string
}

type LimitRequest struct {
	ServiceID string
	Path      string
	Method    string
	IP        string
	UserID    string
	AppID     string
}

type LimitResult struct {
	Allowed    bool
	Remaining  int
	ResetAt    int64
	RetryAfter string
	StatusCode int
}

type RegisteredService struct {
	ServiceID string
	AppID     string
	AppSecret string
}

func New(cfg config.Config) *Client {
	return &Client{
		baseURL: strings.TrimRight(cfg.AuthLimitBaseURL, "/"),
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (c *Client) LoginAdmin(ctx context.Context, username, password string) (string, error) {
	var payload struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	payload.Username = username
	payload.Password = password
	var response struct {
		Data struct {
			AccessToken string `json:"accessToken"`
		} `json:"data"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "/api/auth/login", "", payload, &response); err != nil {
		return "", err
	}
	if response.Data.AccessToken == "" {
		return "", errors.New("auth-limit login response missing access token")
	}
	return response.Data.AccessToken, nil
}

func (c *Client) RegisterService(ctx context.Context, adminToken string, cfg config.Config) (RegisteredService, error) {
	var servicePayload = map[string]any{
		"name":                cfg.AuthLimitServiceName,
		"code":                cfg.AuthLimitServiceCode,
		"baseUrl":             cfg.BaseURL,
		"healthPath":          "/health",
		"healthCheckInterval": 30,
	}
	var serviceResponse struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "/api/services", bearer(adminToken), servicePayload, &serviceResponse); err != nil {
		return RegisteredService{}, err
	}

	var appPayload = map[string]any{"name": cfg.AuthLimitAppName}
	var appResponse struct {
		Data struct {
			AppID     string `json:"appId"`
			AppSecret string `json:"appSecret"`
		} `json:"data"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "/api/apps", bearer(adminToken), appPayload, &appResponse); err != nil {
		return RegisteredService{}, err
	}

	return RegisteredService{
		ServiceID: serviceResponse.Data.ID,
		AppID:     appResponse.Data.AppID,
		AppSecret: appResponse.Data.AppSecret,
	}, nil
}

func (c *Client) VerifyBearer(ctx context.Context, token string) (AuthResult, error) {
	var response struct {
		Data struct {
			UserID      string   `json:"userId"`
			Roles       []string `json:"roles"`
			Permissions []string `json:"permissions"`
		} `json:"data"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/api/auth/verify", bearer(token), nil, &response); err != nil {
		return AuthResult{}, err
	}
	return AuthResult{
		Allowed:     true,
		UserID:      response.Data.UserID,
		Roles:       response.Data.Roles,
		Permissions: response.Data.Permissions,
	}, nil
}

func (c *Client) VerifyM2M(ctx context.Context, headers http.Header, path string, params map[string]any) (AuthResult, error) {
	body := map[string]any{
		"path":   path,
		"method": "GET",
	}
	for key, value := range params {
		body[key] = value
	}
	req, err := c.newJSONRequest(ctx, http.MethodPost, "/api/auth/m2m?path="+url.QueryEscape(path), body)
	if err != nil {
		return AuthResult{}, err
	}
	copyHeader(req.Header, headers, "appId")
	copyHeader(req.Header, headers, "timestamp")
	copyHeader(req.Header, headers, "sign")

	var response struct {
		Data struct {
			AppID   string `json:"appId"`
			Allowed bool   `json:"allowed"`
		} `json:"data"`
	}
	if err := c.do(req, &response); err != nil {
		return AuthResult{}, err
	}
	if !response.Data.Allowed {
		return AuthResult{}, errors.New("auth-limit m2m denied request")
	}
	return AuthResult{Allowed: true, AppID: response.Data.AppID}, nil
}

func (c *Client) VerifyLimit(ctx context.Context, request LimitRequest) (LimitResult, error) {
	payload := map[string]any{
		"serviceId": request.ServiceID,
		"path":      request.Path,
		"method":    request.Method,
		"ip":        request.IP,
		"userId":    request.UserID,
		"appId":     request.AppID,
	}
	req, err := c.newJSONRequest(ctx, http.MethodPost, "/oidc/limit/verify", payload)
	if err != nil {
		return LimitResult{}, err
	}

	res, err := c.httpClient.Do(req)
	if err != nil {
		return LimitResult{}, err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusTooManyRequests || res.StatusCode == http.StatusForbidden {
		return LimitResult{
			Allowed:    false,
			RetryAfter: res.Header.Get("Retry-After"),
			StatusCode: res.StatusCode,
		}, nil
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
		return LimitResult{}, fmt.Errorf("auth-limit limit verify failed: status=%d body=%s", res.StatusCode, string(body))
	}

	var response struct {
		Allowed   bool  `json:"allowed"`
		Remaining int   `json:"remaining"`
		ResetAt   int64 `json:"resetAt"`
	}
	if err := json.NewDecoder(res.Body).Decode(&response); err != nil {
		return LimitResult{}, err
	}
	return LimitResult{
		Allowed:    response.Allowed,
		Remaining:  response.Remaining,
		ResetAt:    response.ResetAt,
		StatusCode: res.StatusCode,
	}, nil
}

func SignM2M(appSecret, timestamp string, params map[string]any) string {
	keys := make([]string, 0, len(params))
	for key, value := range params {
		switch value.(type) {
		case string, int, int64, float64, bool:
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)

	parts := []string{appSecret, timestamp}
	for _, key := range keys {
		parts = append(parts, key+"="+fmt.Sprint(params[key]))
	}
	canonical := strings.Join(parts, "&")
	mac := hmac.New(sha256.New, []byte(appSecret))
	mac.Write([]byte(canonical))
	return hex.EncodeToString(mac.Sum(nil))
}

func Timestamp() string {
	return strconv.FormatInt(time.Now().Unix(), 10)
}

func (c *Client) doJSON(ctx context.Context, method, path, authorization string, payload any, target any) error {
	req, err := c.newJSONRequest(ctx, method, path, payload)
	if err != nil {
		return err
	}
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	return c.do(req, target)
}

func (c *Client) newJSONRequest(ctx context.Context, method, path string, payload any) (*http.Request, error) {
	var body io.Reader
	if payload != nil {
		bodyBytes, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(bodyBytes)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

func (c *Client) do(req *http.Request, target any) error {
	res, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
		return fmt.Errorf("auth-limit request failed: status=%d body=%s", res.StatusCode, string(body))
	}
	if target == nil {
		return nil
	}
	return json.NewDecoder(res.Body).Decode(target)
}

func bearer(token string) string {
	if token == "" {
		return ""
	}
	return "Bearer " + token
}

func copyHeader(dst, src http.Header, key string) {
	if value := src.Get(key); value != "" {
		dst.Set(key, value)
	}
}

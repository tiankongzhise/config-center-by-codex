package authlimit

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
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
	ServiceID        string
	AppID            string
	AppSecret        string
	OperatorPassword string
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

type bootstrapResult struct {
	UserID   string
	RoleID   string
	Password string
}

func (c *Client) BootstrapOperator(ctx context.Context, adminToken string, cfg config.Config) (bootstrapResult, error) {
	password := cfg.AuthLimitOperatorPassword
	if password == "" {
		generated, err := randomPassword()
		if err != nil {
			return bootstrapResult{}, err
		}
		password = generated
	}

	userID, err := c.ensureUser(ctx, cfg, password)
	if err != nil {
		return bootstrapResult{}, err
	}
	permissionIDs, err := c.permissionIDs(ctx, adminToken, []string{"app:manage", "service:manage", "limit:manage", "statistics:read"})
	if err != nil {
		return bootstrapResult{}, err
	}
	roleID, err := c.ensureRole(ctx, adminToken, cfg, permissionIDs)
	if err != nil {
		return bootstrapResult{}, err
	}
	if err := c.assignUserRoles(ctx, adminToken, userID, []string{roleID}); err != nil {
		return bootstrapResult{}, err
	}
	return bootstrapResult{UserID: userID, RoleID: roleID, Password: password}, nil
}

func (c *Client) RegisterService(ctx context.Context, operatorToken string, cfg config.Config) (RegisteredService, error) {
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
	if err := c.doJSON(ctx, http.MethodPost, "/api/services", bearer(operatorToken), servicePayload, &serviceResponse); err != nil {
		return RegisteredService{}, err
	}

	var appPayload = map[string]any{"name": cfg.AuthLimitAppName}
	var appResponse struct {
		Data struct {
			AppID     string `json:"appId"`
			AppSecret string `json:"appSecret"`
		} `json:"data"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "/api/apps", bearer(operatorToken), appPayload, &appResponse); err != nil {
		return RegisteredService{}, err
	}

	return RegisteredService{
		ServiceID: serviceResponse.Data.ID,
		AppID:     appResponse.Data.AppID,
		AppSecret: appResponse.Data.AppSecret,
	}, nil
}

func (c *Client) ensureUser(ctx context.Context, cfg config.Config, password string) (string, error) {
	payload := map[string]any{
		"username":    cfg.AuthLimitOperatorUsername,
		"password":    password,
		"displayName": cfg.AuthLimitOperatorDisplayName,
	}
	var response struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	err := c.doJSON(ctx, http.MethodPost, "/api/users/register", "", payload, &response)
	if err == nil {
		if response.Data.ID == "" {
			return "", errors.New("auth-limit user register response missing id")
		}
		return response.Data.ID, nil
	}
	if !isConflict(err) {
		return "", err
	}
	token, err := c.LoginAdmin(ctx, cfg.AuthLimitAdmin, cfg.AuthLimitAdminSecret)
	if err != nil {
		return "", err
	}
	users, err := c.users(ctx, token)
	if err != nil {
		return "", err
	}
	for _, user := range users {
		if strings.EqualFold(user.Username, cfg.AuthLimitOperatorUsername) {
			return user.ID, nil
		}
	}
	return "", fmt.Errorf("auth-limit operator user %q already exists but cannot be found", cfg.AuthLimitOperatorUsername)
}

type permissionRecord struct {
	ID   string `json:"id"`
	Code string `json:"code"`
}

func (c *Client) permissionIDs(ctx context.Context, adminToken string, codes []string) ([]string, error) {
	var response struct {
		Data []permissionRecord `json:"data"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/api/permissions", bearer(adminToken), nil, &response); err != nil {
		return nil, err
	}
	byCode := make(map[string]string, len(response.Data))
	for _, permission := range response.Data {
		byCode[permission.Code] = permission.ID
	}
	var ids []string
	for _, code := range codes {
		id := byCode[code]
		if id == "" {
			return nil, fmt.Errorf("auth-limit permission %q not found", code)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

type roleRecord struct {
	ID   string `json:"id"`
	Code string `json:"code"`
}

func (c *Client) ensureRole(ctx context.Context, adminToken string, cfg config.Config, permissionIDs []string) (string, error) {
	payload := map[string]any{
		"code":          cfg.AuthLimitOperatorRoleCode,
		"name":          cfg.AuthLimitOperatorRoleName,
		"description":   "配置中心服务注册、APP 管理、限流和统计读取专用角色",
		"permissionIds": permissionIDs,
	}
	var response struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	err := c.doJSON(ctx, http.MethodPost, "/api/roles", bearer(adminToken), payload, &response)
	if err == nil {
		if response.Data.ID == "" {
			return "", errors.New("auth-limit role create response missing id")
		}
		return response.Data.ID, nil
	}
	if !isConflict(err) {
		return "", err
	}
	roles, err := c.roles(ctx, adminToken)
	if err != nil {
		return "", err
	}
	for _, role := range roles {
		if role.Code == cfg.AuthLimitOperatorRoleCode {
			return role.ID, nil
		}
	}
	return "", fmt.Errorf("auth-limit role %q already exists but cannot be found", cfg.AuthLimitOperatorRoleCode)
}

func (c *Client) assignUserRoles(ctx context.Context, adminToken string, userID string, roleIDs []string) error {
	payload := map[string]any{"roleIds": roleIDs}
	return c.doJSON(ctx, http.MethodPut, "/api/users/"+url.PathEscape(userID)+"/roles", bearer(adminToken), payload, nil)
}

type userRecord struct {
	ID       string `json:"id"`
	Username string `json:"username"`
}

func (c *Client) users(ctx context.Context, adminToken string) ([]userRecord, error) {
	var response struct {
		Data []userRecord `json:"data"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/api/users", bearer(adminToken), nil, &response); err != nil {
		return nil, err
	}
	return response.Data, nil
}

func (c *Client) roles(ctx context.Context, adminToken string) ([]roleRecord, error) {
	var response struct {
		Data []roleRecord `json:"data"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/api/roles", bearer(adminToken), nil, &response); err != nil {
		return nil, err
	}
	return response.Data, nil
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

func isConflict(err error) bool {
	return err != nil && strings.Contains(err.Error(), "status=409")
}

func randomPassword() (string, error) {
	bytes := make([]byte, 24)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return "Aa1!" + base64.RawURLEncoding.EncodeToString(bytes), nil
}

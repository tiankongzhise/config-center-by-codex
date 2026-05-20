package authlimit

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"regexp"
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

type LoginResult struct {
	TokenType             string
	AccessToken           string
	AccessTokenExpiresAt  string
	RefreshToken          string
	RefreshTokenExpiresAt string
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

type serviceRecord struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Code    string `json:"code"`
	BaseURL string `json:"baseUrl"`
}

var authLimitUsernamePattern = regexp.MustCompile(`^[A-Za-z0-9_]{3,20}$`)

func New(cfg config.Config) *Client {
	return &Client{
		baseURL: strings.TrimRight(cfg.AuthLimitBaseURL, "/"),
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (c *Client) Login(ctx context.Context, username, password string) (LoginResult, error) {
	var payload struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	payload.Username = username
	payload.Password = password
	var response struct {
		Data struct {
			TokenType             string `json:"tokenType"`
			AccessToken           string `json:"accessToken"`
			AccessTokenExpiresAt  string `json:"accessTokenExpiresAt"`
			RefreshToken          string `json:"refreshToken"`
			RefreshTokenExpiresAt string `json:"refreshTokenExpiresAt"`
		} `json:"data"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "/api/auth/login", "", payload, &response); err != nil {
		return LoginResult{}, err
	}
	if response.Data.AccessToken == "" {
		return LoginResult{}, errors.New("auth-limit login response missing access token")
	}
	tokenType := response.Data.TokenType
	if tokenType == "" {
		tokenType = "Bearer"
	}
	return LoginResult{
		TokenType:             tokenType,
		AccessToken:           response.Data.AccessToken,
		AccessTokenExpiresAt:  response.Data.AccessTokenExpiresAt,
		RefreshToken:          response.Data.RefreshToken,
		RefreshTokenExpiresAt: response.Data.RefreshTokenExpiresAt,
	}, nil
}

func (c *Client) LoginAdmin(ctx context.Context, username, password string) (string, error) {
	result, err := c.Login(ctx, username, password)
	if err != nil {
		return "", err
	}
	return result.AccessToken, nil
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
	if err := validateOperatorCredentials(cfg.AuthLimitOperatorUsername, password); err != nil {
		return bootstrapResult{}, err
	}

	userID, err := c.ensureUser(ctx, cfg, password)
	if err != nil {
		return bootstrapResult{}, fmt.Errorf("bootstrap auth-limit operator user: %w", err)
	}
	permissionIDs, err := c.permissionIDs(ctx, adminToken, []string{"app:manage", "service:manage", "limit:manage", "statistics:read"})
	if err != nil {
		return bootstrapResult{}, fmt.Errorf("load auth-limit permissions: %w", err)
	}
	roleID, err := c.ensureRole(ctx, adminToken, cfg, permissionIDs)
	if err != nil {
		return bootstrapResult{}, fmt.Errorf("bootstrap auth-limit operator role: %w", err)
	}
	if err := c.assignUserRoles(ctx, adminToken, userID, []string{roleID}); err != nil {
		return bootstrapResult{}, fmt.Errorf("assign auth-limit operator role: %w", err)
	}
	return bootstrapResult{UserID: userID, RoleID: roleID, Password: password}, nil
}

func (c *Client) RegisterService(ctx context.Context, operatorToken string, cfg config.Config) (RegisteredService, error) {
	serviceID := strings.TrimSpace(cfg.AuthLimitServiceID)
	appID := strings.TrimSpace(cfg.AuthLimitAppID)
	appSecret := strings.TrimSpace(cfg.AuthLimitAppSecret)

	if serviceID == "" {
		createdServiceID, err := c.ensureService(ctx, operatorToken, cfg)
		if err != nil {
			return RegisteredService{}, err
		}
		serviceID = createdServiceID
	}

	switch {
	case appID == "" && appSecret == "":
		createdAppID, createdAppSecret, err := c.createApp(ctx, operatorToken, cfg)
		if err != nil {
			return RegisteredService{}, err
		}
		appID = createdAppID
		appSecret = createdAppSecret
	case appID == "" || appSecret == "":
		return RegisteredService{}, errors.New("AUTH_LIMIT_APP_ID and AUTH_LIMIT_APP_SECRET must be configured together; app secrets cannot be recovered after creation")
	}

	return RegisteredService{
		ServiceID: serviceID,
		AppID:     appID,
		AppSecret: appSecret,
	}, nil
}

func (c *Client) ensureService(ctx context.Context, operatorToken string, cfg config.Config) (string, error) {
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
	err := c.doJSON(ctx, http.MethodPost, "/api/services", bearer(operatorToken), servicePayload, &serviceResponse)
	if err == nil {
		if serviceResponse.Data.ID == "" {
			return "", errors.New("auth-limit service register response missing id")
		}
		return serviceResponse.Data.ID, nil
	}
	if !isConflict(err) {
		return "", fmt.Errorf("register auth-limit service: %w", err)
	}
	serviceID, findErr := c.findServiceID(ctx, operatorToken, cfg)
	if findErr != nil {
		return "", fmt.Errorf("register auth-limit service: %w; %v", err, findErr)
	}
	return serviceID, nil
}

func (c *Client) createApp(ctx context.Context, operatorToken string, cfg config.Config) (string, string, error) {
	var appPayload = map[string]any{"name": cfg.AuthLimitAppName}
	var appResponse struct {
		Data struct {
			AppID     string `json:"appId"`
			AppSecret string `json:"appSecret"`
		} `json:"data"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "/api/apps", bearer(operatorToken), appPayload, &appResponse); err != nil {
		if isConflict(err) {
			return "", "", errors.New("auth-limit app already exists; fill AUTH_LIMIT_APP_ID and AUTH_LIMIT_APP_SECRET, or reset the app secret in auth-limit and update .env")
		}
		return "", "", fmt.Errorf("create auth-limit app: %w", err)
	}
	if appResponse.Data.AppID == "" || appResponse.Data.AppSecret == "" {
		return "", "", errors.New("auth-limit app create response missing appId or appSecret")
	}
	return appResponse.Data.AppID, appResponse.Data.AppSecret, nil
}

func (c *Client) findServiceID(ctx context.Context, operatorToken string, cfg config.Config) (string, error) {
	queries := []string{cfg.AuthLimitServiceName, cfg.AuthLimitServiceCode, ""}
	for _, query := range queries {
		path := "/api/services"
		if query != "" {
			path += "?name=" + url.QueryEscape(query)
		}
		var response struct {
			Data []serviceRecord `json:"data"`
		}
		if err := c.doJSON(ctx, http.MethodGet, path, bearer(operatorToken), nil, &response); err != nil {
			return "", err
		}
		for _, service := range response.Data {
			if service.ID == "" {
				continue
			}
			if service.Code == cfg.AuthLimitServiceCode || service.Name == cfg.AuthLimitServiceName {
				return service.ID, nil
			}
		}
	}
	return "", fmt.Errorf("auth-limit service %q already exists but cannot be found", cfg.AuthLimitServiceCode)
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
	if err := c.do(req, target); err != nil {
		return fmt.Errorf("auth-limit %s %s: %w", method, path, err)
	}
	return nil
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
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*"
	var builder strings.Builder
	builder.WriteString("Aa1!")
	max := big.NewInt(int64(len(alphabet)))
	for builder.Len() < 20 {
		index, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		builder.WriteByte(alphabet[index.Int64()])
	}
	return builder.String(), nil
}

func validateOperatorCredentials(username, password string) error {
	if !authLimitUsernamePattern.MatchString(username) {
		return fmt.Errorf("AUTH_LIMIT_OPERATOR_USERNAME must match ^[A-Za-z0-9_]{3,20}$ for auth-limit, got %q", username)
	}
	if !validAuthLimitPassword(password) {
		return errors.New("AUTH_LIMIT_OPERATOR_PASSWORD must be 8-20 chars and contain uppercase, lowercase, digit and special character")
	}
	return nil
}

func validAuthLimitPassword(password string) bool {
	if len(password) < 8 || len(password) > 20 {
		return false
	}
	var hasUpper, hasLower, hasDigit, hasSpecial bool
	for _, r := range password {
		switch {
		case r >= 'A' && r <= 'Z':
			hasUpper = true
		case r >= 'a' && r <= 'z':
			hasLower = true
		case r >= '0' && r <= '9':
			hasDigit = true
		default:
			hasSpecial = true
		}
	}
	return hasUpper && hasLower && hasDigit && hasSpecial
}

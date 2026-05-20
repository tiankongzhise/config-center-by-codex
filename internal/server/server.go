package server

import (
	"encoding/json"
	"errors"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/tiankongzhise/config-center-by-codex/internal/app"
	"github.com/tiankongzhise/config-center-by-codex/internal/authlimit"
	"github.com/tiankongzhise/config-center-by-codex/internal/config"
	"github.com/tiankongzhise/config-center-by-codex/internal/store"
)

const sessionCookieName = "config_center_session"

type Server struct {
	cfg      config.Config
	auth     *app.Service
	gateway  *authlimit.Client
	projects *app.ProjectService
	configs  *app.ConfigService
	store    *store.Store
	views    *template.Template
}

func New(cfg config.Config, store *store.Store) *Server {
	return &Server{
		cfg:      cfg,
		store:    store,
		auth:     app.NewService(store),
		gateway:  authlimit.New(cfg),
		projects: app.NewProjectService(store),
		configs:  app.NewConfigService(store),
		views:    template.Must(template.ParseFS(templatesFS, "templates/*.html")),
	}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.health)
	mux.HandleFunc("GET /static/styles.css", s.styles)
	mux.HandleFunc("GET /login", s.loginPage)
	mux.HandleFunc("GET /register", s.registerPage)
	mux.HandleFunc("GET /projects", s.projectsPage)
	mux.HandleFunc("GET /projects/new", s.newProjectPage)
	mux.HandleFunc("GET /projects/{id}", s.projectPage)
	mux.HandleFunc("POST /api/auth/register", s.register)
	mux.HandleFunc("POST /api/auth/login", s.login)
	mux.HandleFunc("POST /api/auth/logout", s.logout)
	mux.HandleFunc("GET /api/auth/me", s.me)
	mux.HandleFunc("POST /api/auth/tokens", s.issueAPIToken)
	mux.HandleFunc("POST /api/auth/tokens/current", s.issueAPITokenForCurrentUser)
	mux.HandleFunc("POST /api/auth/tokens/refresh", s.refreshAPIToken)
	mux.HandleFunc("GET /api/projects", s.listProjects)
	mux.HandleFunc("POST /api/projects", s.createProject)
	mux.HandleFunc("GET /api/projects/{id}", s.getProject)
	mux.HandleFunc("PUT /api/projects/{id}", s.updateProject)
	mux.HandleFunc("DELETE /api/projects/{id}", s.deleteProject)
	mux.HandleFunc("GET /api/projects/{id}/config", s.getProjectConfig)
	mux.HandleFunc("PUT /api/projects/{id}/config", s.saveProjectConfig)
	mux.HandleFunc("GET /api/projects/{id}/env", s.getProjectEnv)
	mux.HandleFunc("PUT /api/projects/{id}/env", s.saveProjectEnv)
	mux.HandleFunc("GET /api/public/projects/{code}/config", s.getPublicProjectConfig)
	mux.HandleFunc("GET /api/public/projects/{code}/env", s.getPublicProjectEnv)
	mux.HandleFunc("GET /", s.index)
	return mux
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"time":   time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *Server) index(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/projects", http.StatusFound)
}

func (s *Server) styles(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	http.ServeFileFS(w, r, assetsFS, "assets/styles.css")
}

func (s *Server) loginPage(w http.ResponseWriter, r *http.Request) {
	s.render(w, "login.html", map[string]any{"Title": "登录"})
}

func (s *Server) registerPage(w http.ResponseWriter, r *http.Request) {
	s.render(w, "register.html", map[string]any{"Title": "注册"})
}

func (s *Server) projectsPage(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireUserPage(w, r)
	if !ok {
		return
	}
	projects, err := s.projects.List(r.Context(), user)
	if err != nil {
		http.Error(w, "加载项目失败", http.StatusInternalServerError)
		return
	}
	s.render(w, "projects.html", map[string]any{
		"Title":    "项目",
		"User":     user,
		"Projects": projects,
	})
}

func (s *Server) newProjectPage(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireUserPage(w, r)
	if !ok {
		return
	}
	s.render(w, "project_new.html", map[string]any{
		"Title": "新建项目",
		"User":  user,
	})
}

func (s *Server) projectPage(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireUserPage(w, r)
	if !ok {
		return
	}
	project, err := s.projects.Get(r.Context(), user, r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	configValue, _ := s.configs.GetForOwner(r.Context(), user, project.ID, "config")
	envValue, _ := s.configs.GetForOwner(r.Context(), user, project.ID, "env")
	s.render(w, "project_detail.html", map[string]any{
		"Title":          project.Name,
		"User":           user,
		"Project":        project,
		"Config":         configValue,
		"Env":            envValue,
		"PublicURL":      strings.TrimRight(s.cfg.BaseURL, "/") + "/api/public/projects/" + project.Code,
		"AuthLimitReady": s.cfg.AuthLimitServiceID != "",
	})
}

func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Username    string `json:"username"`
		Password    string `json:"password"`
		DisplayName string `json:"displayName"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}

	user, err := s.auth.Register(r.Context(), app.RegisterInput{
		Username:    input.Username,
		Password:    input.Password,
		DisplayName: input.DisplayName,
	})
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			writeError(w, http.StatusConflict, "username already exists")
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"user": user})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}

	result, err := s.auth.Login(r.Context(), input.Username, input.Password)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}
	s.setSessionCookie(w, result.Token, result.ExpiresAt)
	writeJSON(w, http.StatusOK, map[string]any{
		"user":      result.User,
		"expiresAt": result.ExpiresAt,
	})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	token := sessionToken(r)
	if err := s.auth.Logout(r.Context(), token); err != nil {
		writeError(w, http.StatusInternalServerError, "logout failed")
		return
	}
	clearSessionCookie(w)
	writeJSON(w, http.StatusOK, map[string]any{"loggedOut": true})
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": user})
}

func (s *Server) issueAPIToken(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}

	result, err := s.auth.IssueAPITokenForCredentials(r.Context(), input.Username, input.Password)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}
	writeJSON(w, http.StatusOK, apiTokenPayload(result))
}

func (s *Server) issueAPITokenForCurrentUser(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	result, err := s.auth.IssueAPITokenForUser(r.Context(), user)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "issue token failed")
		return
	}
	writeJSON(w, http.StatusOK, apiTokenPayload(result))
}

func (s *Server) refreshAPIToken(w http.ResponseWriter, r *http.Request) {
	var input struct {
		RefreshToken string `json:"refreshToken"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	result, err := s.auth.RefreshAPIToken(r.Context(), strings.TrimSpace(input.RefreshToken))
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid refresh token")
		return
	}
	writeJSON(w, http.StatusOK, apiTokenPayload(result))
}

func (s *Server) listProjects(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	projects, err := s.projects.List(r.Context(), user)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list projects failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"projects": projects})
}

func (s *Server) createProject(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	var input app.CreateProjectInput
	if !decodeJSON(w, r, &input) {
		return
	}
	project, err := s.projects.Create(r.Context(), user, input)
	if err != nil {
		writeStoreOrValidationError(w, err, "project code already exists")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"project": project})
}

func (s *Server) getProject(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	project, err := s.projects.Get(r.Context(), user, r.PathValue("id"))
	if err != nil {
		writeStoreOrValidationError(w, err, "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"project": project})
}

func (s *Server) updateProject(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	var input app.UpdateProjectInput
	if !decodeJSON(w, r, &input) {
		return
	}
	project, err := s.projects.Update(r.Context(), user, r.PathValue("id"), input)
	if err != nil {
		writeStoreOrValidationError(w, err, "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"project": project})
}

func (s *Server) deleteProject(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	if err := s.projects.Delete(r.Context(), user, r.PathValue("id")); err != nil {
		writeStoreOrValidationError(w, err, "")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) getProjectConfig(w http.ResponseWriter, r *http.Request) {
	s.getManagedConfig(w, r, "config")
}

func (s *Server) saveProjectConfig(w http.ResponseWriter, r *http.Request) {
	s.saveManagedConfig(w, r, "config")
}

func (s *Server) getProjectEnv(w http.ResponseWriter, r *http.Request) {
	s.getManagedConfig(w, r, "env")
}

func (s *Server) saveProjectEnv(w http.ResponseWriter, r *http.Request) {
	s.saveManagedConfig(w, r, "env")
}

func (s *Server) getManagedConfig(w http.ResponseWriter, r *http.Request, kind string) {
	user, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	cfg, err := s.configs.GetForOwner(r.Context(), user, r.PathValue("id"), kind)
	if err != nil {
		writeStoreOrValidationError(w, err, "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"config": cfg})
}

func (s *Server) saveManagedConfig(w http.ResponseWriter, r *http.Request, kind string) {
	user, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	var input struct {
		Plaintext string `json:"plaintext"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	cfg, err := s.configs.Save(r.Context(), user, r.PathValue("id"), app.SaveConfigInput{
		Kind:      kind,
		Plaintext: input.Plaintext,
	})
	if err != nil {
		writeStoreOrValidationError(w, err, "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"config": cfg})
}

func (s *Server) getPublicProjectConfig(w http.ResponseWriter, r *http.Request) {
	s.getPublicConfig(w, r, "config")
}

func (s *Server) getPublicProjectEnv(w http.ResponseWriter, r *http.Request) {
	s.getPublicConfig(w, r, "env")
}

func (s *Server) getPublicConfig(w http.ResponseWriter, r *http.Request, kind string) {
	user, ok := s.verifyExternalCaller(w, r)
	if !ok {
		return
	}
	limitResult, ok := s.verifyExternalLimit(w, r, user)
	if !ok {
		return
	}

	project, cfg, err := s.configs.GetByProjectCodeForOwner(r.Context(), user, r.PathValue("code"), kind)
	if err != nil {
		writeStoreOrValidationError(w, err, "")
		return
	}
	if limitResult.Remaining >= 0 {
		w.Header().Set("X-RateLimit-Remaining", intToString(limitResult.Remaining))
	}
	if limitResult.ResetAt > 0 {
		w.Header().Set("X-RateLimit-Reset", int64ToString(limitResult.ResetAt))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"project": map[string]any{
			"code": project.Code,
			"name": project.Name,
		},
		"config": cfg,
	})
}

func (s *Server) verifyExternalCaller(w http.ResponseWriter, r *http.Request) (app.User, bool) {
	authHeader := r.Header.Get("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		user, _, err := s.auth.CurrentAPIUser(r.Context(), strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer ")))
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid access token")
			return app.User{}, false
		}
		return user, true
	}
	writeError(w, http.StatusUnauthorized, "Authorization: Bearer <ACCESS_TOKEN> is required")
	return app.User{}, false
}

func (s *Server) verifyExternalLimit(w http.ResponseWriter, r *http.Request, caller app.User) (authlimit.LimitResult, bool) {
	if s.cfg.AuthLimitServiceID == "" {
		writeError(w, http.StatusServiceUnavailable, "AUTH_LIMIT_SERVICE_ID is not configured")
		return authlimit.LimitResult{}, false
	}
	result, err := s.gateway.VerifyLimit(r.Context(), authlimit.LimitRequest{
		ServiceID: s.cfg.AuthLimitServiceID,
		Path:      r.URL.Path,
		Method:    r.Method,
		IP:        clientIP(r),
		UserID:    caller.ID,
	})
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "auth-limit limit verification failed")
		return authlimit.LimitResult{}, false
	}
	if !result.Allowed {
		if result.RetryAfter != "" {
			w.Header().Set("Retry-After", result.RetryAfter)
		}
		status := result.StatusCode
		if status == 0 {
			status = http.StatusTooManyRequests
		}
		writeError(w, status, "request is limited")
		return authlimit.LimitResult{}, false
	}
	return result, true
}

func apiTokenPayload(result app.APITokenResult) map[string]any {
	return map[string]any{
		"tokenType":             result.TokenType,
		"accessToken":           result.AccessToken,
		"accessTokenExpiresAt":  result.AccessTokenExpiresAt,
		"refreshToken":          result.RefreshToken,
		"refreshTokenExpiresAt": result.RefreshTokenExpiresAt,
		"user": map[string]any{
			"id":          result.User.ID,
			"username":    result.User.Username,
			"displayName": result.User.DisplayName,
		},
	}
}

func (s *Server) requireUser(w http.ResponseWriter, r *http.Request) (app.User, bool) {
	user, err := s.auth.CurrentUser(r.Context(), sessionToken(r))
	if err != nil {
		writeError(w, http.StatusUnauthorized, "login required")
		return app.User{}, false
	}
	return user, true
}

func (s *Server) requireUserPage(w http.ResponseWriter, r *http.Request) (app.User, bool) {
	user, err := s.auth.CurrentUser(r.Context(), sessionToken(r))
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return app.User{}, false
	}
	return user, true
}

func (s *Server) setSessionCookie(w http.ResponseWriter, token string, expiresAt time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expiresAt,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.isSecureCookie(),
	})
}

func (s *Server) isSecureCookie() bool {
	return len(s.cfg.BaseURL) >= 8 && s.cfg.BaseURL[:8] == "https://"
}

func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func sessionToken(r *http.Request) string {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return ""
	}
	return cookie.Value
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{
		"error": message,
	})
}

func writeStoreOrValidationError(w http.ResponseWriter, err error, conflictMessage string) {
	switch {
	case errors.Is(err, store.ErrConflict):
		if conflictMessage == "" {
			conflictMessage = "resource already exists"
		}
		writeError(w, http.StatusConflict, conflictMessage)
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "resource not found")
	default:
		writeError(w, http.StatusBadRequest, err.Error())
	}
}

func (s *Server) render(w http.ResponseWriter, name string, data map[string]any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.views.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func clientIP(r *http.Request) string {
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		parts := strings.Split(forwarded, ",")
		return strings.TrimSpace(parts[0])
	}
	if realIP := r.Header.Get("X-Real-IP"); realIP != "" {
		return realIP
	}
	host, _, ok := strings.Cut(r.RemoteAddr, ":")
	if ok {
		return host
	}
	return r.RemoteAddr
}

func pathKind(path string) string {
	if strings.HasSuffix(path, "/env") {
		return "env"
	}
	return "config"
}

func intToString(value int) string {
	return int64ToString(int64(value))
}

func int64ToString(value int64) string {
	return strconv.FormatInt(value, 10)
}

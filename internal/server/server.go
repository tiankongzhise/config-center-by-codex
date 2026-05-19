package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/tiankongzhise/config-center-by-codex/internal/app"
	"github.com/tiankongzhise/config-center-by-codex/internal/config"
	"github.com/tiankongzhise/config-center-by-codex/internal/store"
)

const sessionCookieName = "config_center_session"

type Server struct {
	cfg      config.Config
	auth     *app.Service
	projects *app.ProjectService
	configs  *app.ConfigService
	store    *store.Store
}

func New(cfg config.Config, store *store.Store) *Server {
	return &Server{
		cfg:      cfg,
		store:    store,
		auth:     app.NewService(store),
		projects: app.NewProjectService(store),
		configs:  app.NewConfigService(store),
	}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.health)
	mux.HandleFunc("POST /api/auth/register", s.register)
	mux.HandleFunc("POST /api/auth/login", s.login)
	mux.HandleFunc("POST /api/auth/logout", s.logout)
	mux.HandleFunc("GET /api/auth/me", s.me)
	mux.HandleFunc("GET /api/projects", s.listProjects)
	mux.HandleFunc("POST /api/projects", s.createProject)
	mux.HandleFunc("GET /api/projects/{id}", s.getProject)
	mux.HandleFunc("PUT /api/projects/{id}", s.updateProject)
	mux.HandleFunc("DELETE /api/projects/{id}", s.deleteProject)
	mux.HandleFunc("GET /api/projects/{id}/config", s.getProjectConfig)
	mux.HandleFunc("PUT /api/projects/{id}/config", s.saveProjectConfig)
	mux.HandleFunc("GET /api/projects/{id}/env", s.getProjectEnv)
	mux.HandleFunc("PUT /api/projects/{id}/env", s.saveProjectEnv)
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
	http.Redirect(w, r, "/health", http.StatusFound)
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

func (s *Server) requireUser(w http.ResponseWriter, r *http.Request) (app.User, bool) {
	user, err := s.auth.CurrentUser(r.Context(), sessionToken(r))
	if err != nil {
		writeError(w, http.StatusUnauthorized, "login required")
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

package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"golang.org/x/crypto/bcrypt"

	"pipelogiq/internal/store"
	"pipelogiq/internal/types"
)

func (s *Server) registerUserRoutes(r chi.Router) {
	r.Get("/", s.handleListUsers)
	r.Get("/roles", s.handleListUserRoles)
	r.Get("/applications", s.handleListUserApplications)
	r.Post("/", s.handleCreateUser)
	r.Put("/{id}", s.handleUpdateUser)
	r.Post("/{id}/password", s.handleResetUserPassword)
}

func (s *Server) handleListUserRoles(w http.ResponseWriter, r *http.Request) {
	type role struct {
		ID          string   `json:"id"`
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Permissions []string `json:"permissions"`
	}
	writeJSON(w, []role{
		{"admin", "Admin", "Manage users, access, settings and assigned applications.", []string{"pipelines:read", "pipelines:operate", "schedules:read", "schedules:operate", "users:manage", "settings:manage", "audit:read"}},
		{"operator", "Operator", "Monitor and operate pipelines and schedules in assigned applications.", []string{"pipelines:read", "pipelines:operate", "schedules:read", "schedules:operate"}},
		{"viewer", "Viewer", "Read-only access to pipelines and schedules in assigned applications.", []string{"pipelines:read", "schedules:read"}},
	}, http.StatusOK)
}

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	users, err := s.store.ListUsers(ctx)
	if err != nil {
		s.userManagementError(w, err)
		return
	}
	writeJSON(w, users, http.StatusOK)
}

func (s *Server) handleListUserApplications(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	apps, err := s.store.ListUserApplications(ctx)
	if err != nil {
		s.userManagementError(w, err)
		return
	}
	writeJSON(w, apps, http.StatusOK)
}

// Limit and strictly decode account mutations so omitted fields or typos cannot
// silently replace a user's access with Go zero values.
func decodeUserRequest(w http.ResponseWriter, r *http.Request, dst any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		writeJSONError(w, "invalid request body", http.StatusBadRequest)
		return false
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		writeJSONError(w, "request must contain one JSON object", http.StatusBadRequest)
		return false
	}
	return true
}

type saveUserBody struct {
	types.SaveUserRequest
	IsActive *bool `json:"isActive"`
}

func (req *saveUserBody) validate() error {
	req.FirstName = strings.TrimSpace(req.FirstName)
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	req.Role = types.NormalizeUserRole(req.Role)
	if req.FirstName == "" || len(req.FirstName) > 255 {
		return errors.New("first name is required and must be at most 255 bytes")
	}
	if req.LastName != nil {
		name := strings.TrimSpace(*req.LastName)
		if len(name) > 255 {
			return errors.New("last name must be at most 255 bytes")
		}
		req.LastName = &name
	}
	address, err := mail.ParseAddress(req.Email)
	if err != nil || address.Address != req.Email || len(req.Email) > 255 {
		return errors.New("a valid email address is required")
	}
	if !types.IsValidUserRole(req.Role) {
		return errors.New("role must be admin, operator, or viewer")
	}
	if req.IsActive == nil || req.ApplicationIDs == nil {
		return errors.New("isActive and applicationIds are required")
	}
	req.SaveUserRequest.IsActive = *req.IsActive
	if len(req.ApplicationIDs) > 1000 {
		return errors.New("too many applications")
	}
	return nil
}

func validateUserPassword(password string) error {
	if utf8.RuneCountInString(password) < minPasswordLength {
		return fmt.Errorf("password must contain at least %d characters", minPasswordLength)
	}
	if len(password) > 72 {
		return errors.New("password is too long; choose a shorter password")
	}
	return nil
}

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		saveUserBody
		Password string `json:"password"`
	}
	if !decodeUserRequest(w, r, &req) {
		return
	}
	if err := req.validate(); err != nil {
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := validateUserPassword(req.Password); err != nil {
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		s.userManagementError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	user, err := s.store.CreateUser(ctx, req.SaveUserRequest, string(hash))
	if err != nil {
		s.userManagementError(w, err)
		return
	}
	s.audit(r, "user.create", strconv.Itoa(user.ID), "ok", map[string]any{"role": user.Role, "isActive": user.IsActive, "applicationIds": user.ApplicationIDs})
	writeJSON(w, user, http.StatusCreated)
}

func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	id, ok := userRequestID(w, r)
	if !ok {
		return
	}
	var req saveUserBody
	if !decodeUserRequest(w, r, &req) {
		return
	}
	if err := req.validate(); err != nil {
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	user, err := s.store.UpdateUser(ctx, getUserIDFromContext(r.Context()), id, req.SaveUserRequest)
	if err != nil {
		s.userManagementError(w, err)
		return
	}
	s.hub.DisconnectUser(id)
	s.audit(r, "user.update", strconv.Itoa(id), "ok", map[string]any{"role": user.Role, "isActive": user.IsActive, "applicationIds": user.ApplicationIDs})
	writeJSON(w, user, http.StatusOK)
}

func (s *Server) handleResetUserPassword(w http.ResponseWriter, r *http.Request) {
	id, ok := userRequestID(w, r)
	if !ok {
		return
	}
	if id == getUserIDFromContext(r.Context()) {
		writeJSONError(w, "use your account settings to change your own password", http.StatusBadRequest)
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if !decodeUserRequest(w, r, &req) {
		return
	}
	if err := validateUserPassword(req.Password); err != nil {
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	if _, err := s.store.GetUserByID(ctx, id); err != nil {
		s.userManagementError(w, err)
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		s.userManagementError(w, err)
		return
	}
	if _, err := s.store.UpdatePassword(ctx, id, string(hash)); err != nil {
		s.userManagementError(w, err)
		return
	}
	s.hub.DisconnectUser(id)
	s.audit(r, "user.password_reset", strconv.Itoa(id), "ok", nil)
	w.WriteHeader(http.StatusNoContent)
}

func userRequestID(w http.ResponseWriter, r *http.Request) (int, bool) {
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil || id <= 0 {
		writeJSONError(w, "user not found", http.StatusNotFound)
		return 0, false
	}
	return id, true
}

func (s *Server) userManagementError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	message := "user management request failed"
	switch {
	case errors.Is(err, store.ErrUserNotFound):
		status, message = http.StatusNotFound, "user not found"
	case errors.Is(err, store.ErrEmailInUse):
		status, message = http.StatusConflict, "a user with this email already exists"
	case errors.Is(err, store.ErrLastAdmin):
		status, message = http.StatusConflict, "at least one active administrator is required"
	case errors.Is(err, store.ErrSelfDeactivation):
		status, message = http.StatusConflict, "you cannot deactivate your own account or change your own role"
	case errors.Is(err, store.ErrInvalidApplication):
		status, message = http.StatusBadRequest, "one or more applications do not exist"
	default:
		s.logger.Error("user management failed", "err", err)
	}
	writeJSONError(w, message, status)
}

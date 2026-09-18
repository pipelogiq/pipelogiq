package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"pipelogiq/internal/store"
	"pipelogiq/internal/types"
)

const (
	authCookieName = "authKey"
	jwtIssuer      = "Pipelogiq"
)

type jwtClaims struct {
	UserID       int    `json:"userId"`
	Email        string `json:"email"`
	Role         string `json:"role"`
	TokenVersion int    `json:"tv"`
	jwt.RegisteredClaims
}

const (
	loginWindow       = 5 * time.Minute
	loginPerIP        = 30
	loginPerEmail     = 10
	minPasswordLength = 12
)

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	outcome, status, target := "failed", http.StatusBadRequest, "sign-in"
	defer func() {
		s.audit(r, "auth.login", target, outcome, map[string]any{"status": status})
	}()

	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}

	if !decodeUserRequest(w, r, &req) {
		return
	}

	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	if len(req.Email) <= 255 && req.Email != "" {
		target = req.Email
		// Before credentials are verified this is an attempted identity only;
		// userId stays empty so failed attempts cannot impersonate an actor.
		r = r.WithContext(context.WithValue(r.Context(), userEmailKey, req.Email))
	}
	if req.Email == "" || req.Password == "" {
		http.Error(w, "email and password are required", http.StatusBadRequest)
		return
	}
	if len(req.Email) > 255 || len(req.Password) > 72 {
		writeJSONError(w, "email or password is too long", http.StatusBadRequest)
		return
	}

	if ok, retryAfter := s.loginLimiter.allow(r.RemoteAddr, req.Email); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())+1))
		outcome, status = "rate_limited", http.StatusTooManyRequests
		writeJSONError(w, "too many sign-in attempts; try again later", http.StatusTooManyRequests)
		return
	}
	status = http.StatusUnauthorized

	user, storedHash, err := s.store.GetUserByEmail(ctx, req.Email)
	if err != nil {
		// Perform bcrypt work for unknown accounts using the same work factor
		// as passwords created by this API.
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(req.Password))
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(storedHash), []byte(req.Password)); err != nil || !user.IsActive || store.IsLegacyDemoPasswordHash(storedHash) {
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}
	r = withAuditIdentity(r, user.ID, user.Email)

	s.loginLimiter.reset(req.Email)
	if err := s.issueSession(w, r, user); err != nil {
		status = http.StatusInternalServerError
		s.logger.Error("generate jwt failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	outcome, status = "ok", http.StatusOK
	w.WriteHeader(http.StatusOK)
}

// Keep this work factor aligned with bcrypt.DefaultCost used for account writes.
var dummyHash = []byte("$2a$10$C6UzMDM.H6dfI/f/IKcEeO5Uv3Ye3fHhB0P7mkR6cBcd0yq8lmB6e")

// issueSession mints a JWT for the user and sets the session cookie.
func (s *Server) issueSession(w http.ResponseWriter, r *http.Request, user *types.UserResponse) error {
	token, err := generateJWT(s.jwtSecret, user.ID, user.Email, user.Role, user.TokenVersion)
	if err != nil {
		return err
	}
	// #nosec G124 -- Secure follows COOKIE_SECURE/TLS; local HTTP development is supported.
	http.SetCookie(w, &http.Cookie{
		Name:     authCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.cookieSecure(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   86400 * 7, // 7 days
	})
	return nil
}

// cookieSecure decides the Secure flag. Behind a TLS-terminating proxy r.TLS is
// nil, so "auto" also trusts X-Forwarded-Proto; operators can force either way.
func (s *Server) cookieSecure(r *http.Request) bool {
	switch s.cfg.CookieSecure {
	case "always", "true":
		return true
	case "never", "false":
		return false
	default:
		return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
	}
}

func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	userID := getUserIDFromContext(r.Context())
	if userID == 0 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}
	if !decodeUserRequest(w, r, &req) {
		return
	}
	if err := validateUserPassword(req.NewPassword); err != nil {
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}
	if len(req.CurrentPassword) > 72 {
		writeJSONError(w, "current password is too long", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	user, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	_, storedHash, err := s.store.GetUserByEmail(ctx, user.Email)
	if err != nil || bcrypt.CompareHashAndPassword([]byte(storedHash), []byte(req.CurrentPassword)) != nil {
		s.audit(r, "auth.password", user.Email, "failed", nil)
		writeJSONError(w, "current password is incorrect", http.StatusForbidden)
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	// Compare against the authenticated request's version, not the newer user
	// row: a logout can happen after middleware accepted this request.
	claimedVersion, _ := r.Context().Value(tokenVersionKey).(int)
	version, err := s.store.ChangeOwnPassword(ctx, userID, claimedVersion, storedHash, string(hash))
	if errors.Is(err, store.ErrSessionChanged) {
		writeJSONError(w, "session expired; sign in again", http.StatusUnauthorized)
		return
	}
	if err != nil {
		s.logger.Error("update password failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Every other session is now invalid; keep this one alive with a fresh token.
	user.TokenVersion = version
	s.hub.DisconnectUser(userID)
	if err := s.issueSession(w, r, user); err != nil {
		s.logger.Error("reissue session failed", "err", err)
	}
	s.audit(r, "auth.password", user.Email, "ok", nil)
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleGetCurrentUser(w http.ResponseWriter, r *http.Request) {
	userID := getUserIDFromContext(r.Context())
	if userID == 0 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	user, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}

	writeJSON(w, user, http.StatusOK)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	// Rotating the token version ends every session of the account, not only the
	// one holding this cookie. A stolen token stops working at logout.
	outcome, target := "ok", "session"
	if cookie, err := r.Cookie(authCookieName); err == nil {
		if claims, err := parseJWT(s.jwtSecret, cookie.Value); err == nil {
			ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
			defer cancel()
			user, err := s.store.GetUserByID(ctx, claims.UserID)
			if err != nil || !user.IsActive || user.TokenVersion != claims.TokenVersion {
				outcome = "denied"
			} else {
				r = withAuditIdentity(r, user.ID, user.Email)
				target = "user:" + strconv.Itoa(user.ID)
				if _, err := s.store.BumpTokenVersion(ctx, claims.UserID); err != nil {
					outcome = "failed"
					s.logger.Error("revoke sessions on logout failed", "userId", claims.UserID, "err", err)
				} else {
					s.hub.DisconnectUser(claims.UserID)
				}
			}
		} else {
			outcome = "denied"
		}
	}
	s.audit(r, "auth.logout", target, outcome, map[string]any{"status": http.StatusOK})

	s.clearSessionCookie(w, r)
	w.WriteHeader(http.StatusOK)
}

func (s *Server) clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	// #nosec G124 -- Match the session's configurable Secure flag when deleting it.
	http.SetCookie(w, &http.Cookie{
		Name:     authCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   s.cookieSecure(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

func withAuditIdentity(r *http.Request, userID int, email string) *http.Request {
	ctx := context.WithValue(r.Context(), userIDKey, userID)
	ctx = context.WithValue(ctx, userEmailKey, email)
	return r.WithContext(ctx)
}

func generateJWT(jwtSecret []byte, userID int, email, role string, tokenVersion int) (string, error) {
	claims := jwtClaims{
		UserID:       userID,
		Email:        email,
		Role:         role,
		TokenVersion: tokenVersion,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    jwtIssuer,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(7 * 24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(jwtSecret)
}

func parseJWT(jwtSecret []byte, tokenString string) (*jwtClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &jwtClaims{}, func(token *jwt.Token) (interface{}, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, errors.New("unexpected signing method")
		}
		return jwtSecret, nil
	}, jwt.WithIssuer(jwtIssuer), jwt.WithExpirationRequired())

	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(*jwtClaims); ok && token.Valid {
		return claims, nil
	}

	return nil, jwt.ErrSignatureInvalid
}

// Auth middleware
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(authCookieName)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		claims, err := parseJWT(s.jwtSecret, cookie.Value)
		if err != nil {
			// Clear invalid cookie
			s.clearSessionCookie(w, r)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		ctx := context.WithValue(r.Context(), userIDKey, claims.UserID)
		ctx = context.WithValue(ctx, userRoleKey, normalizeRole(claims.Role))
		ctx = context.WithValue(ctx, userEmailKey, claims.Email)
		ctx = context.WithValue(ctx, tokenVersionKey, claims.TokenVersion)
		ctx = context.WithValue(ctx, tokenExpiresAtKey, claims.ExpiresAt.Time)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) requireAdminMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isAdminRole(getUserRoleFromContext(r.Context())) {
			writeJSONError(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) requireOperatorMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		role := types.NormalizeUserRole(getUserRoleFromContext(r.Context()))
		if role != "admin" && role != "operator" {
			writeJSONError(w, "operator or administrator access is required", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

type contextKey string

const userIDKey contextKey = "userID"
const userRoleKey contextKey = "userRole"
const userEmailKey contextKey = "userEmail"
const tokenVersionKey contextKey = "tokenVersion"
const tokenExpiresAtKey contextKey = "tokenExpiresAt"

func getUserIDFromContext(ctx context.Context) int {
	if userID, ok := ctx.Value(userIDKey).(int); ok {
		return userID
	}
	return 0
}

func getUserRoleFromContext(ctx context.Context) string {
	if role, ok := ctx.Value(userRoleKey).(string); ok {
		return role
	}
	return ""
}

func normalizeRole(role string) string {
	return strings.ToLower(strings.TrimSpace(role))
}

func isAdminRole(role string) bool {
	switch normalizeRole(role) {
	case "admin", "administrator", "superadmin":
		return true
	default:
		return false
	}
}

func writeJSONError(w http.ResponseWriter, message string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

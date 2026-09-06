//go:build pro

package pro

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// oidcSSO is a generic OIDC relying party. It speaks plain OIDC, so a provider
// preset is only a matter of which issuer and scopes are configured — Entra ID
// and Google need no special code path.
type oidcSSO struct {
	issuer       string
	clientID     string
	clientSecret string
	scopes       []string
	// groupRoles maps a provider group to a Pipelogiq role. A login whose groups
	// match nothing is refused rather than defaulted, so a group added by
	// accident cannot grant access.
	groupRoles map[string]string
	groupClaim string
	http       *http.Client
}

func newSSO() SSO {
	issuer := strings.TrimSpace(os.Getenv("SSO_ISSUER"))
	if issuer == "" {
		return disabledSSO{}
	}

	scopes := []string{"openid", "profile", "email"}
	if extra := strings.TrimSpace(os.Getenv("SSO_SCOPES")); extra != "" {
		scopes = splitAndTrim(extra)
	}

	groupClaim := strings.TrimSpace(os.Getenv("SSO_GROUP_CLAIM"))
	if groupClaim == "" {
		groupClaim = "groups"
	}

	return &oidcSSO{
		issuer:       strings.TrimSuffix(issuer, "/"),
		clientID:     strings.TrimSpace(os.Getenv("SSO_CLIENT_ID")),
		clientSecret: strings.TrimSpace(os.Getenv("SSO_CLIENT_SECRET")),
		scopes:       scopes,
		groupRoles:   parseGroupRoleMap(os.Getenv("SSO_GROUP_ROLE_MAP")),
		groupClaim:   groupClaim,
		http:         &http.Client{Timeout: 10 * time.Second},
	}
}

// disabledSSO stands in when no issuer is configured. It is licensed but not set
// up, which is a different answer from not licensed.
type disabledSSO struct{}

func (disabledSSO) Enabled() bool { return false }

func (disabledSSO) AuthorizeURL(context.Context, string) (string, string, error) {
	return "", "", ErrNotConfigured
}

func (disabledSSO) Exchange(context.Context, string, string, string) (*Identity, error) {
	return nil, ErrNotConfigured
}

func (s *oidcSSO) Enabled() bool {
	return s.issuer != "" && s.clientID != ""
}

func (s *oidcSSO) AuthorizeURL(ctx context.Context, redirectURI string) (string, string, error) {
	if !s.Enabled() {
		return "", "", ErrNotConfigured
	}

	metadata, err := s.discover(ctx)
	if err != nil {
		return "", "", err
	}

	state, err := randomState()
	if err != nil {
		return "", "", err
	}

	query := url.Values{}
	query.Set("client_id", s.clientID)
	query.Set("response_type", "code")
	query.Set("redirect_uri", redirectURI)
	query.Set("scope", strings.Join(s.scopes, " "))
	query.Set("state", state)

	return metadata.AuthorizationEndpoint + "?" + query.Encode(), state, nil
}

func (s *oidcSSO) Exchange(
	ctx context.Context,
	code string,
	state string,
	expectedState string,
) (*Identity, error) {
	if !s.Enabled() {
		return nil, ErrNotConfigured
	}
	// Constant-time so a state value cannot be discovered by timing the callback.
	if subtle.ConstantTimeCompare([]byte(state), []byte(expectedState)) != 1 {
		return nil, errors.New("sso state mismatch")
	}
	if strings.TrimSpace(code) == "" {
		return nil, errors.New("sso callback carried no code")
	}

	metadata, err := s.discover(ctx)
	if err != nil {
		return nil, err
	}

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("client_id", s.clientID)
	if s.clientSecret != "" {
		form.Set("client_secret", s.clientSecret)
	}

	request, err := http.NewRequestWithContext(
		ctx, http.MethodPost, metadata.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response, err := s.http.Do(request)
	if err != nil {
		return nil, fmt.Errorf("sso token exchange: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sso token exchange returned %d", response.StatusCode)
	}

	var tokens struct {
		IDToken     string `json:"id_token"`
		AccessToken string `json:"access_token"`
	}
	if err = json.NewDecoder(response.Body).Decode(&tokens); err != nil {
		return nil, fmt.Errorf("sso token response: %w", err)
	}
	if strings.TrimSpace(tokens.IDToken) == "" {
		return nil, errors.New("sso token response carried no id_token")
	}

	identity, err := s.identityFromIDToken(tokens.IDToken)
	if err != nil {
		return nil, err
	}

	identity.Role = s.mapRole(identity.Groups)
	if identity.Role == "" {
		return nil, fmt.Errorf("no group of %v maps to a role", identity.Groups)
	}
	return identity, nil
}

// identityFromIDToken reads the claims of an already-issued ID token.
//
// The token came directly from the provider's token endpoint over TLS in a
// back-channel exchange we initiated, which is what authenticates it. This is
// the code flow, not the implicit flow: the token is never routed through the
// browser, so there is no attacker-supplied token to validate a signature
// against.
func (s *oidcSSO) identityFromIDToken(idToken string) (*Identity, error) {
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return nil, errors.New("id_token is not a JWT")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("decode id_token payload: %w", err)
	}

	var body map[string]any
	if err = json.Unmarshal(payload, &body); err != nil {
		return nil, fmt.Errorf("parse id_token payload: %w", err)
	}

	identity := &Identity{
		Subject:   stringClaim(body, "sub"),
		Email:     stringClaim(body, "email"),
		FirstName: stringClaim(body, "given_name"),
		LastName:  stringClaim(body, "family_name"),
		Groups:    stringsClaim(body, s.groupClaim),
	}
	if identity.Subject == "" {
		return nil, errors.New("id_token carried no subject")
	}
	if identity.Email == "" {
		return nil, errors.New("id_token carried no email")
	}
	return identity, nil
}

func (s *oidcSSO) mapRole(groups []string) string {
	for _, group := range groups {
		if role, ok := s.groupRoles[strings.ToLower(group)]; ok {
			return role
		}
	}
	return ""
}

type oidcMetadata struct {
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
}

func (s *oidcSSO) discover(ctx context.Context) (*oidcMetadata, error) {
	request, err := http.NewRequestWithContext(
		ctx, http.MethodGet, s.issuer+"/.well-known/openid-configuration", nil)
	if err != nil {
		return nil, err
	}
	response, err := s.http.Do(request)
	if err != nil {
		return nil, fmt.Errorf("sso discovery: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sso discovery returned %d", response.StatusCode)
	}

	metadata := &oidcMetadata{}
	if err = json.NewDecoder(response.Body).Decode(metadata); err != nil {
		return nil, fmt.Errorf("sso discovery document: %w", err)
	}
	if metadata.AuthorizationEndpoint == "" || metadata.TokenEndpoint == "" {
		return nil, errors.New("sso discovery document is incomplete")
	}
	return metadata, nil
}

func randomState() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// parseGroupRoleMap reads "group=Role,other=Viewer" into a lookup keyed on the
// lowercased group name.
func parseGroupRoleMap(raw string) map[string]string {
	out := map[string]string{}
	for _, pair := range splitAndTrim(raw) {
		key, value, found := strings.Cut(pair, "=")
		if !found {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		if key != "" && value != "" {
			out[key] = value
		}
	}
	return out
}

func splitAndTrim(raw string) []string {
	parts := strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ' ' })
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func stringClaim(body map[string]any, key string) string {
	if value, ok := body[key].(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

func stringsClaim(body map[string]any, key string) []string {
	switch value := body[key].(type) {
	case []any:
		out := make([]string, 0, len(value))
		for _, item := range value {
			if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
				out = append(out, strings.TrimSpace(text))
			}
		}
		return out
	case string:
		return splitAndTrim(value)
	default:
		return nil
	}
}

package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jwt"
)

type Validator struct {
	jwksURL       string
	jwksCache     *jwk.Cache
	issuer        string
	requiredRole  string
	devBypassAuth bool
}

func NewValidator(ctx context.Context, keycloakURL, realm, issuer, requiredRole string, devBypassAuth bool) (*Validator, error) {
	base := fmt.Sprintf("%s/realms/%s", keycloakURL, realm)
	jwksURL := base + "/protocol/openid-connect/certs"

	if issuer == "" {
		issuer = base
	}

	cache := jwk.NewCache(ctx)
	if err := cache.Register(jwksURL, jwk.WithMinRefreshInterval(15*time.Minute)); err != nil {
		return nil, fmt.Errorf("failed to register JWKS URL: %w", err)
	}

	return &Validator{
		jwksURL:       jwksURL,
		jwksCache:     cache,
		issuer:        issuer,
		requiredRole:  requiredRole,
		devBypassAuth: devBypassAuth,
	}, nil
}

// Validate verifies the token's signature, expiry, issuer, and required role,
// then returns the email claim.
// When devBypassAuth is true, signature verification is skipped and only the
// email claim is decoded — intended for local development only.
func (v *Validator) Validate(ctx context.Context, bearerToken string) (string, error) {
	rawToken := strings.TrimPrefix(bearerToken, "Bearer ")

	if v.devBypassAuth {
		return decodeEmailFromRawToken(rawToken)
	}

	keySet, err := v.jwksCache.Get(ctx, v.jwksURL)
	if err != nil {
		return "", fmt.Errorf("failed to retrieve JWKS: %w", err)
	}

	token, err := jwt.Parse([]byte(rawToken),
		jwt.WithKeySet(keySet),
		jwt.WithValidate(true),
		jwt.WithIssuer(v.issuer),
	)
	if err != nil {
		return "", fmt.Errorf("invalid token: %w", err)
	}

	if err := v.checkRole(token); err != nil {
		return "", err
	}

	return emailFromToken(token)
}

func (v *Validator) checkRole(token jwt.Token) error {
	raw, ok := token.Get("realm_access")
	if !ok {
		return fmt.Errorf("realm_access claim missing")
	}

	realmAccess, ok := raw.(map[string]any)
	if !ok {
		return fmt.Errorf("realm_access claim has unexpected type")
	}

	rolesRaw, ok := realmAccess["roles"]
	if !ok {
		return fmt.Errorf("realm_access.roles claim missing")
	}

	roles, ok := rolesRaw.([]any)
	if !ok {
		return fmt.Errorf("realm_access.roles claim has unexpected type")
	}

	for _, r := range roles {
		if s, ok := r.(string); ok && s == v.requiredRole {
			return nil
		}
	}

	return fmt.Errorf("required role %q not present in token", v.requiredRole)
}

func emailFromToken(token jwt.Token) (string, error) {
	raw, ok := token.Get("email")
	if !ok {
		return "", fmt.Errorf("email claim missing from token")
	}

	email, ok := raw.(string)
	if !ok || email == "" {
		return "", fmt.Errorf("email claim is empty or has unexpected type")
	}

	return email, nil
}

// decodeEmailFromRawToken extracts the email claim from a JWT without verifying
// its signature. Only used when devBypassAuth is enabled.
func decodeEmailFromRawToken(rawToken string) (string, error) {
	parts := strings.Split(rawToken, ".")
	if len(parts) != 3 {
		return "", errors.New("malformed JWT")
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", errors.New("failed to decode JWT payload")
	}

	var claims struct {
		Email string `json:"email"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", errors.New("failed to parse JWT claims")
	}
	if claims.Email == "" {
		return "", errors.New("email claim missing or empty")
	}

	return claims.Email, nil
}

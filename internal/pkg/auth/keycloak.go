package auth

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jwt"
)

type Validator struct {
	jwksURL      string
	jwksCache    *jwk.Cache
	issuer       string
	requiredRole string
}

func NewValidator(ctx context.Context, keycloakURL, realm, requiredRole string) (*Validator, error) {
	base := fmt.Sprintf("%s/realms/%s", keycloakURL, realm)
	jwksURL := base + "/protocol/openid-connect/certs"

	cache := jwk.NewCache(ctx)
	if err := cache.Register(jwksURL, jwk.WithMinRefreshInterval(15*time.Minute)); err != nil {
		return nil, fmt.Errorf("failed to register JWKS URL: %w", err)
	}

	if _, err := cache.Refresh(ctx, jwksURL); err != nil {
		return nil, fmt.Errorf("failed to fetch Keycloak JWKS: %w", err)
	}

	return &Validator{
		jwksURL:      jwksURL,
		jwksCache:    cache,
		issuer:       base,
		requiredRole: requiredRole,
	}, nil
}

// Validate verifies the token's signature, expiry, issuer, and required role,
// then returns the email claim.
func (v *Validator) Validate(ctx context.Context, bearerToken string) (string, error) {
	rawToken := strings.TrimPrefix(bearerToken, "Bearer ")

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

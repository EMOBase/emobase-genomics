package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jwt"
)

type Validator struct {
	jwksURL     string
	jwksCache   *jwk.Cache
	issuer      string
	userInfoURL string
	httpClient  *http.Client
}

func NewValidator(ctx context.Context, keycloakURL, realm string) (*Validator, error) {
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
		jwksURL:     jwksURL,
		jwksCache:   cache,
		issuer:      base,
		userInfoURL: base + "/protocol/openid-connect/userinfo",
		httpClient:  &http.Client{Timeout: 10 * time.Second},
	}, nil
}

func (v *Validator) Validate(ctx context.Context, bearerToken string) (string, error) {
	rawToken := strings.TrimPrefix(bearerToken, "Bearer ")

	keySet, err := v.jwksCache.Get(ctx, v.jwksURL)
	if err != nil {
		return "", fmt.Errorf("failed to retrieve JWKS: %w", err)
	}

	if _, err = jwt.Parse([]byte(rawToken),
		jwt.WithKeySet(keySet),
		jwt.WithValidate(true),
		jwt.WithIssuer(v.issuer),
	); err != nil {
		return "", fmt.Errorf("invalid token: %w", err)
	}

	return v.fetchEmail(ctx, rawToken)
}

func (v *Validator) fetchEmail(ctx context.Context, rawToken string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.userInfoURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to build userinfo request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+rawToken)

	resp, err := v.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("userinfo request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("userinfo returned status %d", resp.StatusCode)
	}

	var info struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return "", fmt.Errorf("failed to decode userinfo response: %w", err)
	}
	if info.Email == "" {
		return "", fmt.Errorf("email not present in userinfo response")
	}

	return info.Email, nil
}

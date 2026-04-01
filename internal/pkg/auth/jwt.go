package auth

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
)

// DecodeUsername extracts the username claim from a raw JWT string (with or
// without the "Bearer " prefix). The signature is NOT verified — this assumes
// the token was already validated by the issuer (Keycloak).
func DecodeUsername(bearerToken string) (string, error) {
	token := strings.TrimPrefix(bearerToken, "Bearer ")

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", errors.New("malformed JWT")
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", errors.New("failed to decode JWT payload")
	}

	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", errors.New("failed to parse JWT claims")
	}

	username, ok := claims["username"].(string)
	if !ok || username == "" {
		return "", errors.New("username claim missing or empty")
	}

	return username, nil
}

package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"go.yaml.in/yaml/v3"
)

func serveSpec(t *testing.T, serverURL string) string {
	t.Helper()
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.GET("/docs/openapi.yaml", NewDocsHandler(serverURL).ServeOpenAPISpec)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/docs/openapi.yaml", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	return w.Body.String()
}

func TestServeOpenAPISpecDefaultsToRelativeServer(t *testing.T) {
	body := serveSpec(t, "")

	if !strings.Contains(body, "url: ..") {
		t.Errorf("default spec must keep the relative server URL, got:\n%s", firstServerBlock(body))
	}
}

func TestServeOpenAPISpecAdvertisesConfiguredServer(t *testing.T) {
	const serverURL = "https://example.org/api/genomicsservice/v1"
	body := serveSpec(t, serverURL)

	if strings.Contains(body, "url: ..") {
		t.Errorf("configured spec must not keep the relative default, got:\n%s", firstServerBlock(body))
	}

	var spec struct {
		Servers []struct {
			URL string `yaml:"url"`
		} `yaml:"servers"`
	}
	if err := yaml.Unmarshal([]byte(body), &spec); err != nil {
		t.Fatalf("served spec is not valid YAML: %v", err)
	}
	if len(spec.Servers) != 1 || spec.Servers[0].URL != serverURL {
		t.Errorf("servers = %+v, want exactly %q", spec.Servers, serverURL)
	}
}

// firstServerBlock returns a short excerpt around the servers entry to keep
// failure output readable.
func firstServerBlock(body string) string {
	const marker = "servers:"
	if i := strings.Index(body, marker); i >= 0 {
		end := i + 120
		if end > len(body) {
			end = len(body)
		}
		return body[i:end]
	}
	return "<servers block not found>"
}

package handler

import (
	"bytes"
	"net/http"
	"strconv"
	"strings"

	"github.com/EMOBase/emobase-genomics/docs"
	"github.com/gin-gonic/gin"
)

const swaggerHTML = `<!DOCTYPE html>
<html>
  <head>
    <title>EMOBase Genomics API</title>
    <meta charset="utf-8"/>
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/swagger-ui-dist@latest/swagger-ui.css">
    <style>body { margin: 0; padding: 0; }</style>
  </head>
  <body>
    <div id="swagger-ui"></div>
    <script src="https://cdn.jsdelivr.net/npm/swagger-ui-dist@latest/swagger-ui-bundle.js"></script>
    <script>
      var specUrl = window.location.pathname.replace(/\/+$/, '') + '/openapi.yaml';
      SwaggerUIBundle({ url: specUrl, dom_id: '#swagger-ui', presets: [SwaggerUIBundle.presets.apis, SwaggerUIBundle.SwaggerUIStandalonePreset], layout: 'BaseLayout' });
    </script>
  </body>
</html>`

// openAPIServerPlaceholder is the relative servers entry in docs/openapi.yaml.
// Resolved against the document's own URL, it works behind any proxy prefix.
const openAPIServerPlaceholder = "url: .."

// DocsHandler serves the embedded API documentation.
type DocsHandler struct {
	openAPISpec []byte
}

func NewDocsHandler(openAPIServerURL string) *DocsHandler {
	spec := docs.OpenAPISpec
	if serverURL := strings.TrimSpace(openAPIServerURL); serverURL != "" {
		server := []byte("url: " + strconv.Quote(serverURL))
		spec = bytes.Replace(spec, []byte(openAPIServerPlaceholder), server, 1)
	}

	return &DocsHandler{openAPISpec: spec}
}

func (h *DocsHandler) ServeAPIDocs(c *gin.Context) {
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.String(http.StatusOK, swaggerHTML)
}

func (h *DocsHandler) ServeOpenAPISpec(c *gin.Context) {
	c.Data(http.StatusOK, "application/yaml; charset=utf-8", h.openAPISpec)
}

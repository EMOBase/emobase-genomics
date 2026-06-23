package handler

import (
	"net/http"

	"github.com/EMOBase/emobase-genomics/docs"
	"github.com/gin-gonic/gin"
)

const redocHTML = `<!DOCTYPE html>
<html>
  <head>
    <title>EMOBase Genomics API</title>
    <meta charset="utf-8"/>
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <style>body { margin: 0; padding: 0; }</style>
  </head>
  <body>
    <div id="redoc-container"></div>
    <script src="https://cdn.jsdelivr.net/npm/redoc@latest/bundles/redoc.standalone.js"></script>
    <script>
      var specUrl = window.location.pathname.replace(/\/+$/, '') + '/openapi.yaml';
      Redoc.init(specUrl, {}, document.getElementById('redoc-container'));
    </script>
  </body>
</html>`

func ServeAPIDocs(c *gin.Context) {
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.String(http.StatusOK, redocHTML)
}

func ServeOpenAPISpec(c *gin.Context) {
	c.Data(http.StatusOK, "application/yaml; charset=utf-8", docs.OpenAPISpec)
}

package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	ucassemblyversion "github.com/EMOBase/emobase-genomics/internal/pkg/usecase/assemblyversion"
	"github.com/gin-gonic/gin"
)

// An assembly's name names its upload folder and its JBrowse2 label, which are
// set once, so renaming would leave both on the old name. PATCH refuses it
// rather than ignoring it, so a client never believes it changed.
func TestAssemblyVersionUpdate_NameCannotChange(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.PATCH("/versions/:name/assemblies/:id", NewAssemblyVersionHandler(ucassemblyversion.New(nil, nil, nil, nil, nil, nil, "", "")).Update)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPatch, "/versions/v1/assemblies/7", strings.NewReader(`{"name":"Renamed"}`)))

	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if msg, _ := body["error"].(string); w.Code != http.StatusBadRequest || !strings.Contains(msg, "name cannot be changed") {
		t.Errorf("got %d %v, want 400 name cannot be changed", w.Code, body)
	}
}

package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	ucversion "github.com/EMOBase/emobase-genomics/internal/pkg/usecase/version"
	"github.com/gin-gonic/gin"
)

// An unsafe version name is the admin's input mistake: it must be a 400 that
// says what a name may contain, not a server error.
func TestVersionCreate_InvalidNameIs400(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/versions", NewVersionHandler(ucversion.New(nil, nil, nil, nil, nil, nil, "")).Create)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/versions", strings.NewReader(`{"name":".."}`)))

	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if w.Code != http.StatusBadRequest || body["error"] != ucversion.ErrInvalidVersionName.Error() {
		t.Errorf("got %d %v, want 400 %q", w.Code, body, ucversion.ErrInvalidVersionName.Error())
	}
}

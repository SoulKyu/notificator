package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestSessionMiddlewareCookieAttributes(t *testing.T) {
	tests := []struct {
		name       string
		secure     bool
		wantSecure bool
	}{
		{name: "secure enabled", secure: true, wantSecure: true},
		{name: "secure disabled", secure: false, wantSecure: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			router := gin.New()
			router.Use(SessionMiddleware("test-secret", tt.secure))
			router.GET("/set", func(c *gin.Context) {
				if err := SetSessionValue(c, "user_id", "1"); err != nil {
					c.String(http.StatusInternalServerError, err.Error())
					return
				}
				c.String(http.StatusOK, "ok")
			})

			req := httptest.NewRequest(http.MethodGet, "/set", nil)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			setCookie := rec.Header().Get("Set-Cookie")
			if setCookie == "" {
				t.Fatalf("expected Set-Cookie header, got none")
			}

			if got := strings.Contains(setCookie, "Secure"); got != tt.wantSecure {
				t.Errorf("Secure attribute presence = %v, want %v (Set-Cookie: %s)", got, tt.wantSecure, setCookie)
			}

			if !strings.Contains(setCookie, "SameSite=Lax") {
				t.Errorf("expected SameSite=Lax in Set-Cookie, got: %s", setCookie)
			}
		})
	}
}

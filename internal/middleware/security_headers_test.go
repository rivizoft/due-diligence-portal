package middleware

import (
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"testing"
)

func TestSecurityHeadersBlobPDFFrame(t *testing.T) {
	e := echo.New()
	e.Use(SecurityHeaders())
	e.GET("/", func(c echo.Context) error { return c.NoContent(200) })
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	csp := rec.Header().Get("Content-Security-Policy")
	require.Contains(t, csp, "frame-src 'self' blob:")
	require.Contains(t, csp, "frame-ancestors 'none'")
	require.Contains(t, csp, "connect-src 'self'")
}

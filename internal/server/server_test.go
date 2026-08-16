package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuthMiddleware(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	handler := authMiddleware("0123456789abcdef0123456789abcdef", false, next)
	for _, test := range []struct {
		name, authorization string
		want                int
	}{
		{"missing", "", http.StatusUnauthorized},
		{"wrong", "Bearer wrong", http.StatusUnauthorized},
		{"valid", "Bearer 0123456789abcdef0123456789abcdef", http.StatusNoContent},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "http://wayminder/mcp", nil)
			request.Header.Set("Authorization", test.authorization)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.want {
				t.Fatalf("status = %d, want %d", response.Code, test.want)
			}
		})
	}
}

func TestHostMiddleware(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	handler := hostMiddleware([]string{"wayminder.example.com"}, false, next)
	request := httptest.NewRequest(http.MethodGet, "http://evil.example/healthz", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
	}
}

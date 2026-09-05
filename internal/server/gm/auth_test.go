package gm

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestCredentialsRoundTripAndHTTPProtection(t *testing.T) {
	credentials, err := NewCredentials("admin", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "gm-auth.json")
	if err := SaveCredentials(path, credentials); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadCredentials(path)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.Verify("admin", "correct horse battery staple") || loaded.Verify("admin", "wrong password") {
		t.Fatal("credential verification result is incorrect")
	}
	server := &Server{credentials: loaded}
	handler := server.authentication(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { writer.WriteHeader(http.StatusNoContent) }))
	request := httptest.NewRequest(http.MethodGet, "http://example.test/gm/", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized || response.Header().Get("WWW-Authenticate") == "" {
		t.Fatalf("unauthenticated response = %d, header=%q", response.Code, response.Header().Get("WWW-Authenticate"))
	}
	request = httptest.NewRequest(http.MethodGet, "http://example.test/gm/", nil)
	request.SetBasicAuth("admin", "correct horse battery staple")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("authenticated response = %d", response.Code)
	}
}

func TestCredentialsAllowAnyNonEmptyPassword(t *testing.T) {
	credentials, err := NewCredentials("admin", "1")
	if err != nil {
		t.Fatal(err)
	}
	if !credentials.Verify("admin", "1") || credentials.Verify("admin", "") {
		t.Fatal("one-character GM password did not round trip")
	}
	if _, err := NewCredentials("admin", ""); err == nil {
		t.Fatal("empty GM password was accepted")
	}
}

package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testToken = "0123456789abcdef0123456789abcdef"

// testApp is an app on port 7000 with a temp demo dir and the real repo root.
func testApp(t *testing.T) *app {
	t.Helper()
	return newApp("../..", t.TempDir(), testToken, 7000)
}

// call sends one request through the guard and returns the status code.
func call(h http.Handler, method, path, host, origin, token string) int {
	r := httptest.NewRequest(method, path, strings.NewReader("{}"))
	r.Host = host
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	if token != "" {
		r.Header.Set("X-NL-Token", token)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w.Code
}

func TestGuard(t *testing.T) {
	h := testApp(t).routes()
	cases := []struct {
		name, method, path, host, origin, token string
		want                                    int
	}{
		{"page without token", "GET", "/", "127.0.0.1:7000", "", "", 200},
		{"page via localhost", "GET", "/", "localhost:7000", "", "", 200},
		{"api with token", "GET", "/api/state", "127.0.0.1:7000", "", testToken, 200},
		{"api same origin", "GET", "/api/state", "127.0.0.1:7000", "http://127.0.0.1:7000", testToken, 200},
		{"api without token", "GET", "/api/state", "127.0.0.1:7000", "", "", 403},
		{"api wrong token", "GET", "/api/state", "127.0.0.1:7000", "", strings.Repeat("0", 32), 403},
		{"api short token", "GET", "/api/state", "127.0.0.1:7000", "", testToken[:31], 403},
		{"dns rebinding host", "GET", "/api/state", "evil.example:7000", "", testToken, 403},
		{"other port", "GET", "/", "127.0.0.1:7001", "", "", 403},
		{"foreign origin", "POST", "/api/state", "127.0.0.1:7000", "http://evil.example", testToken, 403},
		{"null origin", "GET", "/api/state", "127.0.0.1:7000", "null", testToken, 403},
		{"https origin", "GET", "/api/state", "127.0.0.1:7000", "https://127.0.0.1:7000", testToken, 403},
	}
	for _, c := range cases {
		if got := call(h, c.method, c.path, c.host, c.origin, c.token); got != c.want {
			t.Errorf("%s: status %d, want %d", c.name, got, c.want)
		}
	}
}

func TestSecurityHeaders(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.Host = "127.0.0.1:7000"
	w := httptest.NewRecorder()
	testApp(t).routes().ServeHTTP(w, r)
	for _, h := range []string{"Content-Security-Policy", "X-Content-Type-Options", "Referrer-Policy"} {
		if w.Header().Get(h) == "" {
			t.Errorf("missing %s", h)
		}
	}
	if w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Error("CORS header set: other origins could read our API")
	}
	if !strings.Contains(w.Body.String(), "NineLives Lab") {
		t.Errorf("page not served: %q", w.Body.String())
	}
}

func TestModulePath(t *testing.T) {
	for in, want := range map[string]string{
		"module ninelives\n\ngo 1.27\n": "ninelives",
		"// c\nmodule \"ninelives\"\n":  "ninelives",
		"go 1.27\n":                     "",
		"module ninelives/sub\n":        "ninelives/sub",
	} {
		if got := modulePath(in); got != want {
			t.Errorf("modulePath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSlotIsExclusive(t *testing.T) {
	a := testApp(t)
	if !a.acquire() {
		t.Fatal("first acquire failed")
	}
	if a.acquire() {
		t.Fatal("second acquire succeeded while the slot is held")
	}
	a.release()
	if !a.acquire() {
		t.Fatal("acquire after release failed")
	}
}

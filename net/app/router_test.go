package app_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/goflower-io/golib/net/app"
)

// -------------------------------------------------------
// Shared helpers (available to all _test.go files in the package)
// -------------------------------------------------------

// do issues method+target against h and returns the recorder.
func do(h http.Handler, method, target string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(method, target, nil)
	h.ServeHTTP(w, r)
	return w
}

// echo returns a handler that writes s to the response body.
func echo(s string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, s) }
}

// orderMW returns a Middleware that appends name+":in" / name+":out" to *log.
func orderMW(log *[]string, name string) app.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			*log = append(*log, name+":in")
			next.ServeHTTP(w, r)
			*log = append(*log, name+":out")
		})
	}
}

// -------------------------------------------------------
// HTTP method shortcuts
// -------------------------------------------------------

func TestRouter_GET(t *testing.T) {
	r := app.NewRouter()
	r.GET("/x", echo("GET"))
	w := do(r, "GET", "/x")
	assertCode(t, w, 200)
	assertBody(t, w, "GET")
}

func TestRouter_POST(t *testing.T) {
	r := app.NewRouter()
	r.POST("/x", echo("POST"))
	assertCode(t, do(r, "POST", "/x"), 200)
}

func TestRouter_PUT(t *testing.T) {
	r := app.NewRouter()
	r.PUT("/x", echo("PUT"))
	assertCode(t, do(r, "PUT", "/x"), 200)
}

func TestRouter_DELETE(t *testing.T) {
	r := app.NewRouter()
	r.DELETE("/x", echo("DELETE"))
	assertCode(t, do(r, "DELETE", "/x"), 200)
}

func TestRouter_PATCH(t *testing.T) {
	r := app.NewRouter()
	r.PATCH("/x", echo("PATCH"))
	assertCode(t, do(r, "PATCH", "/x"), 200)
}

// Go 1.22 ServeMux returns 405 when the path matches but the method does not.
func TestRouter_WrongMethod_Returns405(t *testing.T) {
	r := app.NewRouter()
	r.GET("/x", echo("ok"))
	assertCode(t, do(r, "POST", "/x"), 405)
}

func TestRouter_UnknownPath_Returns404(t *testing.T) {
	r := app.NewRouter()
	r.GET("/x", echo("ok"))
	assertCode(t, do(r, "GET", "/missing"), 404)
}

// -------------------------------------------------------
// Handle / HandleFunc / Method
// -------------------------------------------------------

func TestRouter_Handle(t *testing.T) {
	r := app.NewRouter()
	r.Handle("GET /h", http.HandlerFunc(echo("handle")))
	assertBody(t, do(r, "GET", "/h"), "handle")
}

func TestRouter_HandleFunc(t *testing.T) {
	r := app.NewRouter()
	r.HandleFunc("GET /hf", echo("handlefunc"))
	assertBody(t, do(r, "GET", "/hf"), "handlefunc")
}

func TestRouter_Method(t *testing.T) {
	r := app.NewRouter()
	r.Method("GET /m", http.HandlerFunc(echo("method")))
	assertBody(t, do(r, "GET", "/m"), "method")
}

// -------------------------------------------------------
// Group — prefix
// -------------------------------------------------------

func TestRouter_Group_Prefix(t *testing.T) {
	r := app.NewRouter()
	g := r.Group("/api")
	g.GET("/users", echo("users"))

	assertCode(t, do(r, "GET", "/api/users"), 200)
	assertBody(t, do(r, "GET", "/api/users"), "users")
	// bare path without prefix must 404
	assertCode(t, do(r, "GET", "/users"), 404)
}

func TestRouter_Group_Nested(t *testing.T) {
	r := app.NewRouter()
	v1 := r.Group("/api").Group("/v1")
	v1.GET("/ping", echo("pong"))

	assertCode(t, do(r, "GET", "/api/v1/ping"), 200)
	assertBody(t, do(r, "GET", "/api/v1/ping"), "pong")
	assertCode(t, do(r, "GET", "/api/ping"), 404)
}

// Method() inside a Group must also receive the prefix.
func TestRouter_Group_Method_Prefix(t *testing.T) {
	r := app.NewRouter()
	g := r.Group("/api")
	g.Method("GET /items", http.HandlerFunc(echo("items")))

	assertCode(t, do(r, "GET", "/api/items"), 200)
	assertBody(t, do(r, "GET", "/api/items"), "items")
}

// Handle() inside a Group with an explicit method token must also get the prefix.
func TestRouter_Group_Handle_Prefix(t *testing.T) {
	r := app.NewRouter()
	g := r.Group("/v2")
	g.Handle("GET /things", http.HandlerFunc(echo("things")))

	assertCode(t, do(r, "GET", "/v2/things"), 200)
}

// -------------------------------------------------------
// Middleware
// -------------------------------------------------------

func TestRouter_Use_GlobalMiddleware(t *testing.T) {
	called := false
	r := app.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			called = true
			next.ServeHTTP(w, req)
		})
	})
	r.GET("/x", echo("ok"))

	do(r, "GET", "/x")
	if !called {
		t.Fatal("global middleware was not called")
	}
}

func TestRouter_MiddlewareOrder(t *testing.T) {
	var log []string
	r := app.NewRouter()
	r.Use(orderMW(&log, "global"))

	g := r.Group("/g", orderMW(&log, "group"))
	g.GET("/r", func(w http.ResponseWriter, req *http.Request) {
		log = append(log, "handler")
	}, orderMW(&log, "route"))

	do(r, "GET", "/g/r")

	want := []string{
		"global:in", "group:in", "route:in",
		"handler",
		"route:out", "group:out", "global:out",
	}
	if !sliceEq(log, want) {
		t.Fatalf("middleware order\n got:  %v\n want: %v", log, want)
	}
}

// Group middleware must NOT run for routes outside the group.
func TestRouter_GroupMiddleware_Isolation(t *testing.T) {
	var log []string
	r := app.NewRouter()

	r.GET("/root", echo("root"))

	g := r.Group("/g", orderMW(&log, "group"))
	g.GET("/route", echo("route"))

	// root route — group middleware must not fire
	log = nil
	do(r, "GET", "/root")
	for _, e := range log {
		if e == "group:in" {
			t.Errorf("group middleware ran for /root: %v", log)
		}
	}

	// group route — group middleware must fire
	log = nil
	do(r, "GET", "/g/route")
	found := false
	for _, e := range log {
		if e == "group:in" {
			found = true
		}
	}
	if !found {
		t.Errorf("group middleware did not run for /g/route: %v", log)
	}
}

// Per-route middleware must only run for that route.
func TestRouter_RouteMiddleware(t *testing.T) {
	var log []string
	r := app.NewRouter()

	r.GET("/a", echo("a"), orderMW(&log, "mw-a"))
	r.GET("/b", echo("b"))

	log = nil
	do(r, "GET", "/a")
	if !contains(log, "mw-a:in") {
		t.Errorf("route middleware did not run for /a: %v", log)
	}

	log = nil
	do(r, "GET", "/b")
	if contains(log, "mw-a:in") {
		t.Errorf("route middleware for /a ran for /b: %v", log)
	}
}

// -------------------------------------------------------
// H adapter
// -------------------------------------------------------

func TestRouter_H_Method(t *testing.T) {
	r := app.NewRouter()
	r.GET("/h", r.H(func(w *app.Writer, req *http.Request) {
		w.Text(200, "via-H")
	}))
	w := do(r, "GET", "/h")
	assertCode(t, w, 200)
	assertBody(t, w, "via-H")
}

func TestPackage_H(t *testing.T) {
	r := app.NewRouter()
	r.GET("/ph", app.H(func(w *app.Writer, req *http.Request) {
		w.Text(200, "pkg-H")
	}))
	assertBody(t, do(r, "GET", "/ph"), "pkg-H")
}

// -------------------------------------------------------
// Context helpers
// -------------------------------------------------------

func TestSetValue_GetValue(t *testing.T) {
	r := app.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := app.SetValue(req.Context(), "key", "value")
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	})
	r.GET("/ctx", func(w http.ResponseWriter, req *http.Request) {
		v, ok := app.GetValue(req.Context(), "key")
		if !ok {
			http.Error(w, "not found", 500)
			return
		}
		fmt.Fprint(w, v)
	})

	assertBody(t, do(r, "GET", "/ctx"), "value")
}

func TestGetValue_Missing(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	_, ok := app.GetValue(req.Context(), "missing")
	if ok {
		t.Fatal("expected ok=false for missing key")
	}
}

// -------------------------------------------------------
// Assertion helpers
// -------------------------------------------------------

func assertCode(t *testing.T, w *httptest.ResponseRecorder, code int) {
	t.Helper()
	if w.Code != code {
		t.Fatalf("expected status %d, got %d (body: %q)", code, w.Code, w.Body.String())
	}
}

func assertBody(t *testing.T, w *httptest.ResponseRecorder, want string) {
	t.Helper()
	if got := w.Body.String(); got != want {
		t.Fatalf("expected body %q, got %q", want, got)
	}
}

func sliceEq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func contains(s []string, v string) bool {
	for _, e := range s {
		if e == v {
			return true
		}
	}
	return false
}

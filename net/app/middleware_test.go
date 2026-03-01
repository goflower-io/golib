package app_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/goflower-io/golib/net/app"
)

// -------------------------------------------------------
// RecoveryMiddle
// -------------------------------------------------------

func TestRecoveryMiddle_Panic(t *testing.T) {
	h := app.RecoveryMiddle(func(w http.ResponseWriter, r *http.Request) {
		panic("test panic")
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)

	// Must not propagate the panic.
	h.ServeHTTP(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 after panic, got %d", w.Code)
	}
}

func TestRecoveryMiddle_NoPanic(t *testing.T) {
	h := app.RecoveryMiddle(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	h.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

// Panics with non-string values (e.g. error) must also be caught.
func TestRecoveryMiddle_PanicError(t *testing.T) {
	h := app.RecoveryMiddle(func(w http.ResponseWriter, r *http.Request) {
		panic(errGeneric("boom"))
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	h.ServeHTTP(w, r)

	if w.Code != 500 {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

// -------------------------------------------------------
// LogMidddle
// -------------------------------------------------------

func TestLogMidddle_DoesNotBreakResponse(t *testing.T) {
	h := app.LogMidddle(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	h.ServeHTTP(w, r)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", w.Code)
	}
}

// -------------------------------------------------------
// StatusRecorder — WriteHeader capture
// -------------------------------------------------------

func TestLogMidddle_Captures200WhenNotExplicit(t *testing.T) {
	// Handler does not call WriteHeader; the HTTP default is 200.
	// LogMidddle should not corrupt the response.
	h := app.LogMidddle(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok")) // implicitly 200
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	h.ServeHTTP(w, r)

	// httptest.ResponseRecorder defaults to 200 even without WriteHeader.
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if w.Body.String() != "ok" {
		t.Fatalf("unexpected body: %q", w.Body.String())
	}
}

// -------------------------------------------------------
// MetricMiddle — re-registration must not panic
// -------------------------------------------------------

func TestMetricMiddle_DoubleRegistration(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("MetricMiddle panicked on second registration: %v", r)
		}
	}()
	app.MetricMiddle("test-svc")
	app.MetricMiddle("test-svc") // must not panic
}

func TestMetricMiddle_Handler(t *testing.T) {
	m := app.MetricMiddle("test-metric-handler")
	h := m.Hander(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	h.ServeHTTP(w, r)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

// -------------------------------------------------------
// Middleware as app.Middleware type (used in Router.Use / Group)
// -------------------------------------------------------

func TestMiddleware_AsRouterMiddleware(t *testing.T) {
	called := false
	mw := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			next.ServeHTTP(w, r)
		})
	}

	router := app.NewRouter()
	router.Use(mw)
	router.GET("/x", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/x", nil)
	router.ServeHTTP(w, r)

	if !called {
		t.Fatal("middleware was not called via router.Use")
	}
}

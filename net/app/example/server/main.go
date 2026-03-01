// Package main is an example server that exercises all features of net/app.
//
// Start:
//
//	go run ./net/app/example/server
//
// Endpoints:
//
//	GET  /ping                    – plain text
//	GET  /api/v1/json             – JSON ok
//	GET  /api/v1/xml              – XML ok
//	GET  /api/v1/text             – plain text
//	GET  /api/v1/html             – raw HTML
//	GET  /api/v1/no-content       – 204
//	GET  /api/v1/errors/not-found – 404
//	GET  /api/v1/errors/forbidden – 403
//	GET  /api/v1/errors/unauth    – 401
//	GET  /api/v1/errors/custom    – custom HTTPError
//	GET  /api/v1/errors/wrapped   – wrapped error
//	GET  /api/v1/negotiate        – content negotiation (Accept header)
//	GET  /api/v1/paginator?page=2 – paginator helper
//	GET  /api/v1/context          – SetValue / GetValue via middleware
//	POST /api/v1/parse/json       – GetRequestParams (JSON body)
//	POST /api/v1/parse/form       – GetRequestParams (form body)
//	GET  /api/v1/group/a          – nested group with extra middleware
//	GET  /api/v1/group/b          – same nested group
//	GET  /api/v1/method           – Method() helper (explicit method+path syntax)
//	GET  /api/v1/std              – standard http.HandlerFunc (no Writer)
package main

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/goflower-io/golib/net/app"
)

// -------------------------------------------------------
// Entry point
// -------------------------------------------------------

func main() {
	a := app.New(
		app.WithAddr("0.0.0.0", 8080),
		app.WithSlogConfig(&app.SLogConfig{
			Level:      slog.LevelDebug,
			JSONOutPut: false,
			AddSource:  false,
		}),
	)

	// Global middleware applied to every route.
	a.Use(requestIDMiddleware)

	// Health check — standard http.HandlerFunc, no Writer needed.
	a.GET("/ping", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("pong"))
	})

	// All API routes share the /api/v1 prefix and the auth middleware.
	api := a.Group("/api/v1", logGroupMiddleware)

	registerResponseRoutes(api, a)
	registerErrorRoutes(api)
	registerUtilRoutes(api)
	registerGroupRoutes(api)

	slog.Info("listening on :8080 — run `curl http://localhost:8080/ping` to start")
	a.Run()
}

// -------------------------------------------------------
// Response helpers demo
// -------------------------------------------------------

func registerResponseRoutes(api *app.Router, a *app.App) {
	// JSON
	api.GET("/json", a.H(func(w *app.Writer, r *http.Request) {
		w.JSONOk(map[string]any{"message": "hello", "ok": true})
	}))

	// XML — must use a named struct; xml.Encoder cannot encode map[string]any.
	type xmlPayload struct {
		Message string `xml:"message"`
		OK      bool   `xml:"ok"`
	}
	api.GET("/xml", a.H(func(w *app.Writer, r *http.Request) {
		w.XMLOk(xmlPayload{Message: "hello", OK: true})
	}))

	// Plain text
	api.GET("/text", a.H(func(w *app.Writer, r *http.Request) {
		w.Text(http.StatusOK, "hello, world")
	}))

	// Raw HTML
	api.GET("/html", a.H(func(w *app.Writer, r *http.Request) {
		w.HTML(http.StatusOK, "<h1>Hello from net/app</h1>")
	}))

	// 204 No Content
	api.GET("/no-content", a.H(func(w *app.Writer, r *http.Request) {
		w.NoContent()
	}))

	// Content negotiation: set Accept header to switch format.
	//   curl -H "Accept: application/xml"          .../negotiate
	//   curl -H "Accept: application/json"         .../negotiate  (default)
	type negotiatePayload struct {
		Negotiated bool `json:"negotiated" xml:"negotiated"`
		Value      int  `json:"value"      xml:"value"`
	}
	api.GET("/negotiate", a.H(func(w *app.Writer, r *http.Request) {
		w.Negotiate(http.StatusOK, negotiatePayload{Negotiated: true, Value: 42}, nil)
	}))

	// Standard http.HandlerFunc — Handle() accepts any http.Handler.
	api.Handle("GET /std", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "standard handler, no Writer")
	}))

	// Method() — explicit "METHOD /path" syntax, prefix is applied automatically.
	api.Method("GET /method", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "registered via Method()")
	}))
}

// -------------------------------------------------------
// Error types demo
// -------------------------------------------------------

func registerErrorRoutes(api *app.Router) {
	errs := api.Group("/errors")

	errs.GET("/not-found", app.H(func(w *app.Writer, r *http.Request) {
		w.NotFound()
	}))

	errs.GET("/forbidden", app.H(func(w *app.Writer, r *http.Request) {
		w.Forbidden()
	}))

	errs.GET("/unauth", app.H(func(w *app.Writer, r *http.Request) {
		w.Unauthorized()
	}))

	// NewError — custom status + message.
	errs.GET("/custom", app.H(func(w *app.Writer, r *http.Request) {
		w.JSONErr(app.NewError(http.StatusTeapot, "I'm a teapot"))
	}))

	// WrapError — hide the underlying cause from the client.
	errs.GET("/wrapped", app.H(func(w *app.Writer, r *http.Request) {
		cause := fmt.Errorf("db connection refused")
		w.JSONErr(app.WrapError(app.ErrInternal, cause))
	}))
}

// -------------------------------------------------------
// Utility helpers demo
// -------------------------------------------------------

// parseBody is a request body for the parse endpoints.
type parseBody struct {
	Name  string `json:"name"  form:"name"  validate:"required"`
	Email string `json:"email" form:"email" validate:"required,email"`
}

func registerUtilRoutes(api *app.Router) {
	// Paginator — try ?page=1..10
	api.GET("/paginator", app.H(func(w *app.Writer, r *http.Request) {
		page := int32(1)
		fmt.Sscan(r.URL.Query().Get("page"), &page)
		p := app.CaculatePaginator(page, 10, 100)
		w.JSONOk(p)
	}))

	// ResponseConentType — detects JSON / HTMX / HTML from headers.
	api.GET("/content-type", app.H(func(w *app.Writer, r *http.Request) {
		rt := app.ResponseConentType(r)
		w.JSONOk(map[string]string{"detected": string(rt)})
	}))

	// SetValue / GetValue — context value passing.
	api.GET("/context", contextValueMiddleware(app.H(func(w *app.Writer, r *http.Request) {
		val, ok := app.GetValue(r.Context(), "demo-key")
		w.JSONOk(map[string]any{"value": val, "found": ok})
	})))

	// GetRequestParams — JSON body.
	//   curl -X POST -H "Content-Type: application/json" \
	//        -d '{"name":"Alice","email":"alice@example.com"}' \
	//        http://localhost:8080/api/v1/parse/json
	api.POST("/parse/json", app.H(func(w *app.Writer, r *http.Request) {
		var body parseBody
		if err := app.GetRequestParams(&body, r); err != nil {
			w.JSONErr(app.WrapError(app.ErrBadRequest, err))
			return
		}
		if err := app.Validator().Struct(body); err != nil {
			w.JSONErr(app.WrapError(app.ErrBadRequest, err))
			return
		}
		w.JSONOk(body)
	}))

	// GetRequestParams — form body.
	//   curl -X POST -d "name=Alice&email=alice@example.com" \
	//        http://localhost:8080/api/v1/parse/form
	api.POST("/parse/form", app.H(func(w *app.Writer, r *http.Request) {
		var body parseBody
		if err := app.GetRequestParams(&body, r); err != nil {
			w.JSONErr(app.WrapError(app.ErrBadRequest, err))
			return
		}
		if err := app.Validator().Struct(body); err != nil {
			w.JSONErr(app.WrapError(app.ErrBadRequest, err))
			return
		}
		w.JSONOk(body)
	}))
}

// -------------------------------------------------------
// Group / nested group demo
// -------------------------------------------------------

func registerGroupRoutes(api *app.Router) {
	// Nested group inherits /api/v1 prefix and logGroupMiddleware,
	// plus its own extraMiddleware.
	g := api.Group("/group", extraMiddleware)

	g.GET("/a", app.H(func(w *app.Writer, r *http.Request) {
		reqID, _ := app.GetValue(r.Context(), "request-id")
		w.JSONOk(map[string]any{"route": "group/a", "request_id": reqID})
	}))

	g.GET("/b", app.H(func(w *app.Writer, r *http.Request) {
		reqID, _ := app.GetValue(r.Context(), "request-id")
		w.JSONOk(map[string]any{"route": "group/b", "request_id": reqID})
	}))
}

// -------------------------------------------------------
// Middlewares
// -------------------------------------------------------

// requestIDMiddleware injects a request ID into the context.
func requestIDMiddleware(next http.Handler) http.Handler {
	counter := 0
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		counter++
		ctx := app.SetValue(r.Context(), "request-id", fmt.Sprintf("req-%d", counter))
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// logGroupMiddleware logs which group handled the request.
func logGroupMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slog.Debug("api/v1 group", "path", r.URL.Path)
		next.ServeHTTP(w, r)
	})
}

// extraMiddleware demonstrates per-group middleware stacking.
func extraMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slog.Debug("extra middleware", "path", r.URL.Path)
		next.ServeHTTP(w, r)
	})
}

// contextValueMiddleware is used as a per-route middleware to inject a value.
func contextValueMiddleware(next http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := app.SetValue(r.Context(), "demo-key", "injected-by-middleware")
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}

package app

import (
	"context"
	"net/http"
	"strings"
)

// -------------------------------------------------------
// Middleware & Handler types
// -------------------------------------------------------

// Middleware is a function that wraps an http.Handler.
type Middleware func(http.Handler) http.Handler

// chain applies middlewares left-to-right around h.
// Execution order: m1 → m2 → m3 → handler.
func chain(h http.Handler, middlewares ...Middleware) http.Handler {
	for i := len(middlewares) - 1; i >= 0; i-- {
		h = middlewares[i](h)
	}
	return h
}

// -------------------------------------------------------
// Router
// -------------------------------------------------------

// Router wraps http.ServeMux with prefix and middleware support.
// All sub-routers created via Group share the same underlying ServeMux.
type Router struct {
	mux         *http.ServeMux
	prefix      string
	middlewares []Middleware
}

// NewRouter creates a root Router with an empty prefix and no middlewares.
func NewRouter() *Router {
	return &Router{
		mux: http.NewServeMux(),
	}
}

// ServeHTTP implements http.Handler.
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mux.ServeHTTP(w, req)
}

// Use appends global middlewares. Must be called before registering routes.
func (r *Router) Use(m ...Middleware) *Router {
	r.middlewares = append(r.middlewares, m...)
	return r
}

// Group returns a new Router scoped to prefix, inheriting parent middlewares.
// Additional middlewares m are appended after the inherited ones.
func (r *Router) Group(prefix string, m ...Middleware) *Router {
	return &Router{
		mux:         r.mux, // shared with parent
		prefix:      r.prefix + prefix,
		middlewares: append(append([]Middleware{}, r.middlewares...), m...),
	}
}

// Handle registers a standard http.Handler at pattern (with prefix applied).
// Use this to integrate third-party handlers.
// Middleware priority: global → group → route.
func (r *Router) Handle(pattern string, h http.Handler, m ...Middleware) {
	fullPattern := r.prefix + pattern
	all := append(append([]Middleware{}, r.middlewares...), m...)
	r.mux.Handle(fullPattern, chain(h, all...))
}

// HandleFunc registers an http.HandlerFunc at pattern.
func (r *Router) HandleFunc(pattern string, h http.HandlerFunc, m ...Middleware) {
	r.Handle(pattern, h, m...)
}

// Method registers a handler using Go 1.22 method+path syntax.
// Example: r.Method("GET /users/{id}", handler)
func (r *Router) Method(pattern string, h http.Handler, m ...Middleware) {
	parts := strings.SplitN(pattern, " ", 2)
	if len(parts) == 2 {
		r.Handle(parts[0]+" "+r.prefix+parts[1], h, m...)
		return
	}
	r.Handle(pattern, h, m...)
}

// GET registers an http.HandlerFunc for GET requests.
func (r *Router) GET(path string, h http.HandlerFunc, m ...Middleware) {
	r.Handle("GET "+path, h, m...)
}

// POST registers an http.HandlerFunc for POST requests.
func (r *Router) POST(path string, h http.HandlerFunc, m ...Middleware) {
	r.Handle("POST "+path, h, m...)
}

// PUT registers an http.HandlerFunc for PUT requests.
func (r *Router) PUT(path string, h http.HandlerFunc, m ...Middleware) {
	r.Handle("PUT "+path, h, m...)
}

// DELETE registers an http.HandlerFunc for DELETE requests.
func (r *Router) DELETE(path string, h http.HandlerFunc, m ...Middleware) {
	r.Handle("DELETE "+path, h, m...)
}

// PATCH registers an http.HandlerFunc for PATCH requests.
func (r *Router) PATCH(path string, h http.HandlerFunc, m ...Middleware) {
	r.Handle("PATCH "+path, h, m...)
}

// -------------------------------------------------------
// Context helpers: pass values between middlewares and handlers
// -------------------------------------------------------

type contextKey string

// SetValue stores a value in the request context under the given key.
func SetValue(ctx context.Context, key string, val any) context.Context {
	return context.WithValue(ctx, contextKey(key), val)
}

// GetValue retrieves a value from the request context by key.
func GetValue(ctx context.Context, key string) (any, bool) {
	val := ctx.Value(contextKey(key))
	return val, val != nil
}

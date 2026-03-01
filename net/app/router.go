package app

import (
	"context"
	"net/http"
	"strings"
)

// -------------------------------------------------------
// 中间件类型
// -------------------------------------------------------

type Middleware func(http.Handler) http.Handler

// chain 将多个中间件从左到右串联
// 执行顺序: m1 → m2 → m3 → handler
func chain(h http.Handler, middlewares ...Middleware) http.Handler {
	// 从右往左包裹，保证执行顺序从左到右
	for i := len(middlewares) - 1; i >= 0; i-- {
		h = middlewares[i](h)
	}
	return h
}

// -------------------------------------------------------
// Router
// -------------------------------------------------------

type Router struct {
	mux         *http.ServeMux
	prefix      string
	middlewares []Middleware
}

// New 创建根路由
func NewRouter() *Router {
	return &Router{
		mux: http.NewServeMux(),
	}
}

// ServeHTTP 实现 http.Handler，可直接传给 http.ListenAndServe
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mux.ServeHTTP(w, req)
}

// Use 注册全局中间件（必须在注册路由之前调用）
func (r *Router) Use(m ...Middleware) *Router {
	r.middlewares = append(r.middlewares, m...)
	return r
}

// Group 创建路由分组，继承父路由的中间件
func (r *Router) Group(prefix string, m ...Middleware) *Router {
	return &Router{
		mux:         r.mux, // 共享同一个 ServeMux
		prefix:      r.prefix + prefix,
		middlewares: append(append([]Middleware{}, r.middlewares...), m...),
	}
}

// Handle 注册路由，自动拼接前缀并应用中间件链
func (r *Router) Handle(pattern string, h http.Handler, m ...Middleware) {
	fullPattern := r.prefix + pattern
	// 路由级中间件优先级: 全局 → 分组 → 路由
	allMiddlewares := append(append([]Middleware{}, r.middlewares...), m...)
	r.mux.Handle(fullPattern, chain(h, allMiddlewares...))
}

// HandleFunc 同 Handle，接受 func
func (r *Router) HandleFunc(pattern string, h http.HandlerFunc, m ...Middleware) {
	r.Handle(pattern, h, m...)
}

// Method 语义化注册，pattern 格式遵循 Go 1.22 路由语法
// 例: r.Method("GET /users/{id}", handler)
func (r *Router) Method(pattern string, h http.Handler, m ...Middleware) {
	// 如果 pattern 已经包含 method 前缀，直接拼接路径部分
	parts := strings.SplitN(pattern, " ", 2)
	if len(parts) == 2 {
		r.Handle(parts[0]+" "+r.prefix+parts[1], h, m...)
		return
	}
	r.Handle(pattern, h, m...)
}

// GET / POST / PUT / DELETE / PATCH 快捷方法
func (r *Router) GET(path string, h http.HandlerFunc, m ...Middleware) {
	r.Handle("GET "+path, h, m...)
}

func (r *Router) POST(path string, h http.HandlerFunc, m ...Middleware) {
	r.Handle("POST "+path, h, m...)
}

func (r *Router) PUT(path string, h http.HandlerFunc, m ...Middleware) {
	r.Handle("PUT "+path, h, m...)
}

func (r *Router) DELETE(path string, h http.HandlerFunc, m ...Middleware) {
	r.Handle("DELETE "+path, h, m...)
}

func (r *Router) PATCH(path string, h http.HandlerFunc, m ...Middleware) {
	r.Handle("PATCH "+path, h, m...)
}

// -------------------------------------------------------
// Context 工具：在中间件和 handler 间传递值
// -------------------------------------------------------

type contextKey string

func SetValue(ctx context.Context, key string, val any) context.Context {
	return context.WithValue(ctx, contextKey(key), val)
}

func GetValue(ctx context.Context, key string) (any, bool) {
	val := ctx.Value(contextKey(key))
	return val, val != nil
}

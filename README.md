# golib — HTTP/gRPC Application Framework for Go

**golib** is the application server and utilities library in the goflower-io ecosystem. It provides a production-ready HTTP/gRPC server, hot-reloadable configuration, unified response helpers, and built-in observability — so you can focus on business logic rather than boilerplate.

[中文文档](README_zh.md) | [crud](https://github.com/goflower-io/crud) | [xsql](https://github.com/goflower-io/xsql) | [example](https://github.com/goflower-io/example)

---

## Packages

| Package | Description |
|---|---|
| `net/app` | HTTP/gRPC application server with routing, middleware, and response helpers |
| `cfg` | Configuration file management with hot-reload |

---

## Installation

```bash
go get github.com/goflower-io/golib
```

---

## net/app — Application Server

### Quick Start

```go
import (
    "net/http"
    "github.com/goflower-io/golib/net/app"
)

func main() {
    a := app.New(app.WithAddr("0.0.0.0", 8080))

    a.GET("/hello", func(w http.ResponseWriter, r *http.Request) {
        w.Write([]byte("hello"))
    })

    a.Run() // blocks; shuts down cleanly on SIGINT/SIGTERM
}
```

### Serve gRPC and HTTP on the Same Port

golib uses [cmux](https://github.com/soheilhy/cmux) to demultiplex gRPC and HTTP/1.1 connections on a single listener. gRPC reflection is registered automatically, so `grpcurl` works out of the box.

```go
import (
    "github.com/goflower-io/golib/net/app"
    "github.com/goflower-io/example/api"
    "github.com/goflower-io/example/service"
)

func main() {
    a := app.New(
        app.WithAddr("0.0.0.0", 8080),
        app.WithPromAddr("127.0.0.1", 9090), // Prometheus metrics
    )

    // Register gRPC service
    svc := &service.UserServiceImpl{Client: db}
    a.RegisteGrpcService(&api.UserService_ServiceDesc, svc)

    // Register HTTP routes
    a.GET("/healthz", func(w http.ResponseWriter, r *http.Request) {
        w.Write([]byte("ok"))
    })

    a.Run()
}
```

### TLS and gRPC-Web

```go
a := app.New(
    app.WithTLSConfig(&app.TLSConfig{
        Addr:     app.Addr{IP: "0.0.0.0", Port: 443},
        CertPath: "./cert.pem",
        KeyPath:  "./key.pem",
    }),
    app.WihtGrpcWeb(true), // enable gRPC-Web for browser clients
    app.WithCorsOptions(&cors.Options{
        AllowedOrigins: []string{"*"},
        AllowedMethods: []string{"*"},
        AllowedHeaders: []string{"*"},
    }),
)
```

---

### Routing

```go
// HTTP method shortcuts
a.GET("/users",    handleListUsers)
a.POST("/users",   handleCreateUser)
a.PUT("/users",    handleUpdateUser)
a.DELETE("/users", handleDeleteUser)

// Route groups with shared prefix and middleware
api := a.Group("/api/v1", authMiddleware)
api.GET("/users",  handleListUsers)
api.POST("/users", handleCreateUser)

// Per-route middleware
a.GET("/admin", handleAdmin, adminOnlyMiddleware)

// Use the Writer helper for typed responses
a.GET("/users/:id", a.H(func(w *app.Writer, r *http.Request) {
    u, err := svc.GetUser(r.Context(), &api.UserId{Id: 1})
    if err != nil {
        w.JSONErr(err)
        return
    }
    w.JSONOk(u)
}))
```

### Response Helpers

The `Writer` type wraps `http.ResponseWriter` with typed, format-aware response methods:

```go
// JSON
w.JSONOk(data)                         // 200 + JSON body
w.JSONErr(err)                         // 500 + error JSON
w.JSON(http.StatusCreated, data)       // custom status

// XML
w.XMLOk(data)
w.XML(http.StatusCreated, data)

// Protobuf
w.ProtoOk(msg)

// Content negotiation (selects proto/xml/json based on Accept header)
w.Negotiate(http.StatusOK, data, protoMsg)

// Templ components (a-h/templ)
w.TemplOk(views.UserListPage(users))

// Shortcuts
w.NoContent()    // 204
w.NotFound()     // 404
w.Forbidden()    // 403
w.Unauthorized() // 401

// Typed errors
w.JSONErr(app.ErrBadRequest)   // 400
w.JSONErr(app.ErrNotFound)     // 404
w.JSONErr(app.NewError(http.StatusConflict, "already exists"))
```

### Built-in Middleware

Automatically applied to every request:

| Middleware | What it does |
|---|---|
| `RecoveryMiddle` | Catches panics, logs stack trace, returns 500 |
| `LogMidddle` | Logs method, path, status code, and duration via `slog` |
| `PromMiddleWare` | Records `http_requests_total` and `http_request_duration_ms` in Prometheus |

Custom middleware:

```go
func AuthMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        token := r.Header.Get("Authorization")
        if token == "" {
            http.Error(w, "unauthorized", http.StatusUnauthorized)
            return
        }
        next.ServeHTTP(w, r)
    })
}

a.Use(AuthMiddleware)          // global
api := a.Group("/api", AuthMiddleware) // group-scoped
```

### Request Parsing

```go
// Decode JSON body or form data into a struct, then validate
type CreateReq struct {
    Name string `json:"name" form:"name" validate:"required,max=100"`
    Age  int    `json:"age"  form:"age"  validate:"min=0,max=150"`
}

a.POST("/users", a.H(func(w *app.Writer, r *http.Request) {
    req := new(CreateReq)
    if err := app.GetRequestParams(req, r); err != nil {
        w.JSONErr(app.ErrBadRequest)
        return
    }
    // req is now populated and validated
}))
```

### Pagination Helper

```go
paginator := app.CaculatePaginator(page, pageSize, totalCount)
// Returns: Pages, TotalPage, Pre, Next, CurrentPage, PageSize
```

### AppOption Reference

```go
app.WithAddr("0.0.0.0", 8080)              // plain TCP listener
app.WithTLSConfig(&app.TLSConfig{...})     // TLS listener
app.WithPromAddr("127.0.0.1", 9090)        // Prometheus /metrics endpoint
app.WihtGrpcWeb(true)                      // enable gRPC-Web proxy
app.WithCorsOptions(&cors.Options{...})    // CORS headers
app.WithSlogConfig(&app.SLogConfig{...})   // custom slog handler
```

---

## cfg — Hot-Reload Configuration

```go
import "github.com/goflower-io/golib/cfg"

// Start cfg with -cfg flag pointing to your config directory:
// ./myapp -cfg ./configs

type AppConfig struct {
    Port int    `toml:"port"`
    DSN  string `toml:"dsn"`
}

var conf AppConfig
cfg.Config().UnmarshalTo("app.toml", &conf)

// Watch for file changes (hot-reload)
ch, _ := cfg.Config().Watch("app.toml")
go func() {
    for content := range ch {
        // Re-parse updated config
        _ = toml.Unmarshal(content.Content, &conf)
    }
}()
```

Supported formats: `.toml`, `.yaml`, `.yml`, `.json`

---

## Testing with grpcurl

Because golib automatically registers gRPC server reflection, you can introspect and call any registered service without a `.proto` file:

```bash
# Install grpcurl
go install github.com/fullstorydev/grpcurl/cmd/grpcurl@latest

# List all registered gRPC services
grpcurl -plaintext localhost:8080 list

# Output:
# UserService
# grpc.health.v1.Health
# grpc.reflection.v1alpha.ServerReflection

# Describe a service
grpcurl -plaintext localhost:8080 describe UserService

# Create a user
grpcurl -plaintext \
  -d '{"name":"alice","age":18}' \
  localhost:8080 UserService/CreateUser

# Get a user by ID
grpcurl -plaintext \
  -d '{"id":1}' \
  localhost:8080 UserService/GetUser

# Update specific fields (field mask)
grpcurl -plaintext \
  -d '{"user":{"id":1,"name":"bob","age":20},"masks":["name","age"]}' \
  localhost:8080 UserService/UpdateUser

# List with pagination + filter (age > 18) + order by id desc
grpcurl -plaintext \
  -d '{"page":1,"page_size":10,"filters":[{"field":3,"op":"GT","val":"18"}],"orderbys":[{"field":1,"desc":true}]}' \
  localhost:8080 UserService/ListUsers

# Delete a user
grpcurl -plaintext \
  -d '{"id":1}' \
  localhost:8080 UserService/DeleteUser

# Health check
grpcurl -plaintext localhost:8080 grpc.health.v1.Health/Check

# With TLS
grpcurl -cacert ./cert.pem \
  -d '{"name":"alice","age":18}' \
  localhost:443 UserService/CreateUser
```

---

## Related

- [crud](https://github.com/goflower-io/crud) — generate gRPC service skeletons from SQL DDL
- [xsql](https://github.com/goflower-io/xsql) — SQL builder and DB client used inside generated services
- [example](https://github.com/goflower-io/example) — full working examples showing all four libraries together

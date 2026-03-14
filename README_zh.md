# golib — Go 语言 HTTP/gRPC 应用框架

**golib** 是 goflower-io 生态中的应用服务器与工具库。提供开箱即用的 HTTP/gRPC 服务器、热重载配置管理、统一响应助手和内置可观测性——让你专注于业务逻辑，而非繁琐的基础设施搭建。

[English](README.md) | [crud](https://github.com/goflower-io/crud) | [xsql](https://github.com/goflower-io/xsql) | [示例代码](https://github.com/goflower-io/example)

---

## 包结构

| 包 | 说明 |
|---|---|
| `net/app` | HTTP/gRPC 应用服务器，含路由、中间件和响应助手 |
| `cfg` | 支持热重载的配置文件管理 |

---

## 安装

```bash
go get github.com/goflower-io/golib
```

---

## net/app — 应用服务器

### 快速开始

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

    a.Run() // 阻塞运行，收到 SIGINT/SIGTERM 时优雅关闭
}
```

### 同端口同时提供 gRPC 和 HTTP 服务

golib 使用 [cmux](https://github.com/soheilhy/cmux) 在单一监听器上自动区分 gRPC 和 HTTP/1.1 连接。同时自动注册 gRPC 反射，`grpcurl` 开箱即用。

```go
import (
    "github.com/goflower-io/golib/net/app"
    "github.com/goflower-io/example/api"
    "github.com/goflower-io/example/service"
)

func main() {
    a := app.New(
        app.WithAddr("0.0.0.0", 8080),
        app.WithPromAddr("127.0.0.1", 9090), // Prometheus 指标
    )

    // 注册 gRPC 服务
    svc := &service.UserServiceImpl{Client: db}
    a.RegisteGrpcService(&api.UserService_ServiceDesc, svc)

    // 注册 HTTP 路由
    a.GET("/healthz", func(w http.ResponseWriter, r *http.Request) {
        w.Write([]byte("ok"))
    })

    a.Run()
}
```

### TLS 与 gRPC-Web

```go
a := app.New(
    app.WithTLSConfig(&app.TLSConfig{
        Addr:     app.Addr{IP: "0.0.0.0", Port: 443},
        CertPath: "./cert.pem",
        KeyPath:  "./key.pem",
    }),
    app.WihtGrpcWeb(true), // 为浏览器客户端开启 gRPC-Web
    app.WithCorsOptions(&cors.Options{
        AllowedOrigins: []string{"*"},
        AllowedMethods: []string{"*"},
        AllowedHeaders: []string{"*"},
    }),
)
```

---

### 路由

```go
// HTTP 方法快捷方式
a.GET("/users",    handleListUsers)
a.POST("/users",   handleCreateUser)
a.PUT("/users",    handleUpdateUser)
a.DELETE("/users", handleDeleteUser)

// 路由分组：共享前缀和中间件
api := a.Group("/api/v1", authMiddleware)
api.GET("/users",  handleListUsers)
api.POST("/users", handleCreateUser)

// 单路由中间件
a.GET("/admin", handleAdmin, adminOnlyMiddleware)

// 使用 Writer 助手实现类型化响应
a.GET("/users/:id", a.H(func(w *app.Writer, r *http.Request) {
    u, err := svc.GetUser(r.Context(), &api.UserId{Id: 1})
    if err != nil {
        w.JSONErr(err)
        return
    }
    w.JSONOk(u)
}))
```

### 响应助手

`Writer` 类型封装了 `http.ResponseWriter`，提供类型化、格式感知的响应方法：

```go
// JSON
w.JSONOk(data)                         // 200 + JSON 响应体
w.JSONErr(err)                         // 500 + 错误 JSON
w.JSON(http.StatusCreated, data)       // 自定义状态码

// XML
w.XMLOk(data)
w.XML(http.StatusCreated, data)

// Protobuf
w.ProtoOk(msg)

// 内容协商（根据 Accept 头自动选择 proto/xml/json）
w.Negotiate(http.StatusOK, data, protoMsg)

// Templ 组件（a-h/templ）
w.TemplOk(views.UserListPage(users))

// 快捷方法
w.NoContent()    // 204
w.NotFound()     // 404
w.Forbidden()    // 403
w.Unauthorized() // 401

// 类型化错误
w.JSONErr(app.ErrBadRequest)   // 400
w.JSONErr(app.ErrNotFound)     // 404
w.JSONErr(app.NewError(http.StatusConflict, "already exists"))
```

### 内置中间件

自动应用于每个请求：

| 中间件 | 作用 |
|---|---|
| `RecoveryMiddle` | 捕获 panic，记录堆栈，返回 500 |
| `LogMidddle` | 通过 `slog` 记录请求方法、路径、状态码和耗时 |
| `PromMiddleWare` | 向 Prometheus 记录 `http_requests_total` 和 `http_request_duration_ms` |

自定义中间件：

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

a.Use(AuthMiddleware)                      // 全局
api := a.Group("/api", AuthMiddleware)    // 分组级别
```

### 请求解析

```go
// 将 JSON 请求体或表单数据解码到结构体，并自动校验
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
    // req 已填充并通过校验
}))
```

### 分页助手

```go
paginator := app.CaculatePaginator(page, pageSize, totalCount)
// 返回：Pages、TotalPage、Pre、Next、CurrentPage、PageSize
```

### AppOption 参数说明

```go
app.WithAddr("0.0.0.0", 8080)              // 纯 TCP 监听器
app.WithTLSConfig(&app.TLSConfig{...})     // TLS 监听器
app.WithPromAddr("127.0.0.1", 9090)        // Prometheus /metrics 端点
app.WihtGrpcWeb(true)                      // 开启 gRPC-Web 代理
app.WithCorsOptions(&cors.Options{...})    // CORS 头
app.WithSlogConfig(&app.SLogConfig{...})   // 自定义 slog 处理器
```

---

## cfg — 热重载配置管理

```go
import "github.com/goflower-io/golib/cfg"

// 启动时通过 -cfg 参数指定配置目录：
// ./myapp -cfg ./configs

type AppConfig struct {
    Port int    `toml:"port"`
    DSN  string `toml:"dsn"`
}

var conf AppConfig
cfg.Config().UnmarshalTo("app.toml", &conf)

// 监听文件变更（热重载）
ch, _ := cfg.Config().Watch("app.toml")
go func() {
    for content := range ch {
        // 重新解析更新后的配置
        _ = toml.Unmarshal(content.Content, &conf)
    }
}()
```

支持格式：`.toml`、`.yaml`、`.yml`、`.json`

---

## 使用 grpcurl 测试

由于 golib 自动注册 gRPC 服务反射，无需 `.proto` 文件即可内省和调用任意已注册服务：

```bash
# 安装 grpcurl
go install github.com/fullstorydev/grpcurl/cmd/grpcurl@latest

# 列出所有已注册的 gRPC 服务
grpcurl -plaintext localhost:8080 list

# 输出：
# UserService
# grpc.health.v1.Health
# grpc.reflection.v1alpha.ServerReflection

# 查看服务接口描述
grpcurl -plaintext localhost:8080 describe UserService

# 创建用户
grpcurl -plaintext \
  -d '{"name":"alice","age":18}' \
  localhost:8080 UserService/CreateUser

# 按 ID 查询用户
grpcurl -plaintext \
  -d '{"id":1}' \
  localhost:8080 UserService/GetUser

# 更新指定字段（字段掩码）
grpcurl -plaintext \
  -d '{"user":{"id":1,"name":"bob","age":20},"masks":["name","age"]}' \
  localhost:8080 UserService/UpdateUser

# 分页查询 + 过滤（age > 18）+ 按 id 倒序
grpcurl -plaintext \
  -d '{"page":1,"page_size":10,"filters":[{"field":3,"op":"GT","val":"18"}],"orderbys":[{"field":1,"desc":true}]}' \
  localhost:8080 UserService/ListUsers

# 删除用户
grpcurl -plaintext \
  -d '{"id":1}' \
  localhost:8080 UserService/DeleteUser

# 健康检查
grpcurl -plaintext localhost:8080 grpc.health.v1.Health/Check

# TLS 模式
grpcurl -cacert ./cert.pem \
  -d '{"name":"alice","age":18}' \
  localhost:443 UserService/CreateUser
```

---

## 相关仓库

- [crud](https://github.com/goflower-io/crud) — 从 SQL DDL 生成 gRPC 服务骨架
- [xsql](https://github.com/goflower-io/xsql) — 生成服务内部使用的 SQL 构建器和 DB 客户端
- [example](https://github.com/goflower-io/example) — 展示四个库协同工作的全栈示例

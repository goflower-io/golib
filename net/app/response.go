package app

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"html/template"
	"net/http"
	"sync"

	"google.golang.org/protobuf/proto"
)

// -------------------------------------------------------
// 统一响应结构
// -------------------------------------------------------

type Response struct {
	Code    int    `json:"code"             xml:"code"`
	Message string `json:"message"          xml:"message"`
	Data    any    `json:"data,omitempty"   xml:"data,omitempty"`
	TraceID string `json:"trace_id,omitempty" xml:"trace_id,omitempty"`
}

// -------------------------------------------------------
// Content-Type 常量
// -------------------------------------------------------

const (
	MIMEJson     = "application/json; charset=utf-8"
	MIMEXml      = "application/xml; charset=utf-8"
	MIMEProtobuf = "application/x-protobuf"
	MIMEHtml     = "text/html; charset=utf-8"
	MIMEText     = "text/plain; charset=utf-8"
)

// -------------------------------------------------------
// Writer：核心写入器
// -------------------------------------------------------

type Writer struct {
	w       http.ResponseWriter
	r       *http.Request
	traceID string
}

// New 创建 Writer，推荐在 handler 入口处调用
func NewWriter(w http.ResponseWriter, r *http.Request) *Writer {
	return &Writer{w: w, r: r, traceID: traceIDFromCtx(r)}
}

// ----- JSON -----

func (wr *Writer) JSON(status int, data any) {
	wr.writeJSON(status, &Response{
		Code:    status,
		Message: http.StatusText(status),
		Data:    data,
		TraceID: wr.traceID,
	})
}

func (wr *Writer) JSONOk(data any) { wr.JSON(http.StatusOK, data) }

func (wr *Writer) JSONErr(err error) {
	he := toHTTPError(err)
	wr.writeJSON(he.Status, &Response{
		Code:    he.Status,
		Message: he.Message,
		TraceID: wr.traceID,
	})
}

func (wr *Writer) writeJSON(status int, resp *Response) {
	wr.w.Header().Set("Content-Type", MIMEJson)
	wr.w.WriteHeader(status)
	if err := json.NewEncoder(wr.w).Encode(resp); err != nil {
		http.Error(wr.w, "json encode error", http.StatusInternalServerError)
	}
}

// ----- XML -----

func (wr *Writer) XML(status int, data any) {
	wr.writeXML(status, &Response{
		Code:    status,
		Message: http.StatusText(status),
		Data:    data,
		TraceID: wr.traceID,
	})
}

func (wr *Writer) XMLOk(data any) { wr.XML(http.StatusOK, data) }

func (wr *Writer) XMLErr(err error) {
	he := toHTTPError(err)
	wr.writeXML(he.Status, &Response{
		Code:    he.Status,
		Message: he.Message,
		TraceID: wr.traceID,
	})
}

func (wr *Writer) writeXML(status int, resp *Response) {
	wr.w.Header().Set("Content-Type", MIMEXml)
	wr.w.WriteHeader(status)
	wr.w.Write([]byte(xml.Header))
	if err := xml.NewEncoder(wr.w).Encode(resp); err != nil {
		http.Error(wr.w, "xml encode error", http.StatusInternalServerError)
	}
}

// ----- Protobuf -----

func (wr *Writer) Proto(status int, msg proto.Message) {
	b, err := proto.Marshal(msg)
	if err != nil {
		wr.JSONErr(ErrInternal)
		return
	}
	wr.w.Header().Set("Content-Type", MIMEProtobuf)
	wr.w.WriteHeader(status)
	wr.w.Write(b)
}

func (wr *Writer) ProtoOk(msg proto.Message) { wr.Proto(http.StatusOK, msg) }

// ----- HTMX -----

// HTML 直接写 HTML 字符串（HTMX 局部片段常用）
func (wr *Writer) HTML(status int, html string) {
	wr.w.Header().Set("Content-Type", MIMEHtml)
	wr.w.WriteHeader(status)
	wr.w.Write([]byte(html))
}

// Template 渲染 html/template 模板
func (wr *Writer) Template(status int, tmpl *template.Template, data any) {
	wr.w.Header().Set("Content-Type", MIMEHtml)
	wr.w.WriteHeader(status)
	if err := tmpl.Execute(wr.w, data); err != nil {
		http.Error(wr.w, "template render error", http.StatusInternalServerError)
	}
}

// ----- 内容协商：根据 Accept 头自动选择格式 -----

func (wr *Writer) Negotiate(status int, data any, protoMsg proto.Message) {
	accept := wr.r.Header.Get("Accept")
	switch {
	case contains(accept, "application/x-protobuf") && protoMsg != nil:
		wr.Proto(status, protoMsg)
	case contains(accept, "application/xml"):
		wr.XML(status, data)
	default:
		wr.JSON(status, data)
	}
}

// ----- 常用快捷方法 -----

func (wr *Writer) NoContent() { wr.w.WriteHeader(http.StatusNoContent) }

func (wr *Writer) NotFound() { wr.JSONErr(ErrNotFound) }

func (wr *Writer) Forbidden() { wr.JSONErr(ErrForbidden) }

func (wr *Writer) Unauthorized() { wr.JSONErr(ErrUnauthorized) }

// ----- Text -----

func (wr *Writer) Text(status int, text string) {
	wr.w.Header().Set("Content-Type", MIMEText)
	wr.w.WriteHeader(status)
	wr.w.Write([]byte(text))
}

// -------------------------------------------------------
// 错误体系
// -------------------------------------------------------

type HTTPError struct {
	Status  int
	Message string
	Err     error
}

func (e *HTTPError) Error() string { return e.Message }

func (e *HTTPError) Unwrap() error { return e.Err }

// 预定义错误
var (
	ErrBadRequest   = &HTTPError{Status: 400, Message: "Bad Request"}
	ErrUnauthorized = &HTTPError{Status: 401, Message: "Unauthorized"}
	ErrForbidden    = &HTTPError{Status: 403, Message: "Forbidden"}
	ErrNotFound     = &HTTPError{Status: 404, Message: "Not Found"}
	ErrConflict     = &HTTPError{Status: 409, Message: "Conflict"}
	ErrInternal     = &HTTPError{Status: 500, Message: "Internal Server Error"}
)

// NewError 自定义错误
func NewError(status int, message string) *HTTPError {
	return &HTTPError{Status: status, Message: message}
}

// WrapError 包装底层错误（不暴露给客户端）
func WrapError(httpErr *HTTPError, cause error) *HTTPError {
	return &HTTPError{Status: httpErr.Status, Message: httpErr.Message, Err: cause}
}

func toHTTPError(err error) *HTTPError {
	var he *HTTPError
	if errors.As(err, &he) {
		return he
	}
	return &HTTPError{Status: 500, Message: "Internal Server Error", Err: err}
}

type TemplateRegistry struct {
	mu        sync.RWMutex
	templates map[string]*template.Template
}

var Templates = &TemplateRegistry{
	templates: make(map[string]*template.Template),
}

func (tr *TemplateRegistry) Register(name string, tmpl *template.Template) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	tr.templates[name] = tmpl
}

func (tr *TemplateRegistry) Get(name string) (*template.Template, bool) {
	tr.mu.RLock()
	defer tr.mu.RUnlock()
	t, ok := tr.templates[name]
	return t, ok
}

// RenderTemplate 通过名称渲染已注册的模板
func (wr *Writer) RenderTemplate(status int, name string, data any) {
	tmpl, ok := Templates.Get(name)
	if !ok {
		wr.JSONErr(NewError(500, "template not found: "+name))
		return
	}
	wr.Template(status, tmpl, data)
}

// -------------------------------------------------------
// 工具函数
// -------------------------------------------------------

func contains(s, sub string) bool {
	return len(sub) > 0 && len(s) >= len(sub) &&
		(s == sub || len(s) > 0 && containsStr(s, sub))
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// traceIDFromCtx 从 context 取 traceID（配合链路追踪中间件使用）
func traceIDFromCtx(r *http.Request) string {
	if id, ok := r.Context().Value(ctxKeyTraceID{}).(string); ok {
		return id
	}
	return ""
}

type ctxKeyTraceID struct{}

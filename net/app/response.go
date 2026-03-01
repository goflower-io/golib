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
// Unified response envelope
// -------------------------------------------------------

// Response is the standard JSON/XML response body returned by all endpoints.
type Response struct {
	Code    int    `json:"code"               xml:"code"`
	Message string `json:"message"            xml:"message"`
	Data    any    `json:"data,omitempty"     xml:"data,omitempty"`
	TraceID string `json:"trace_id,omitempty" xml:"trace_id,omitempty"`
}

// -------------------------------------------------------
// Content-Type constants
// -------------------------------------------------------

const (
	MIMEJson     = "application/json; charset=utf-8"
	MIMEXml      = "application/xml; charset=utf-8"
	MIMEProtobuf = "application/x-protobuf"
	MIMEHtml     = "text/html; charset=utf-8"
	MIMEText     = "text/plain; charset=utf-8"
)

// -------------------------------------------------------
// Writer — core response writer
// -------------------------------------------------------

// Writer wraps http.ResponseWriter and *http.Request to provide typed
// response helpers. Create one per request via NewWriter.
type Writer struct {
	w       http.ResponseWriter
	r       *http.Request
	traceID string
}

// NewWriter creates a Writer. Called automatically when using HandlerFunc.
func NewWriter(w http.ResponseWriter, r *http.Request) *Writer {
	return &Writer{w: w, r: r, traceID: traceIDFromCtx(r)}
}

// ----- JSON -----

// JSON writes a JSON response with the given status code.
func (wr *Writer) JSON(status int, data any) {
	wr.writeJSON(status, &Response{
		Code:    status,
		Message: http.StatusText(status),
		Data:    data,
		TraceID: wr.traceID,
	})
}

// JSONOk writes a 200 JSON response.
func (wr *Writer) JSONOk(data any) { wr.JSON(http.StatusOK, data) }

// JSONErr writes a JSON error response derived from err.
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

// XML writes an XML response with the given status code.
func (wr *Writer) XML(status int, data any) {
	wr.writeXML(status, &Response{
		Code:    status,
		Message: http.StatusText(status),
		Data:    data,
		TraceID: wr.traceID,
	})
}

// XMLOk writes a 200 XML response.
func (wr *Writer) XMLOk(data any) { wr.XML(http.StatusOK, data) }

// XMLErr writes an XML error response derived from err.
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

// Proto writes a protobuf response with the given status code.
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

// ProtoOk writes a 200 protobuf response.
func (wr *Writer) ProtoOk(msg proto.Message) { wr.Proto(http.StatusOK, msg) }

// ----- HTML / Template -----

// HTML writes a raw HTML string with the given status code.
func (wr *Writer) HTML(status int, html string) {
	wr.w.Header().Set("Content-Type", MIMEHtml)
	wr.w.WriteHeader(status)
	wr.w.Write([]byte(html))
}

// Template renders an html/template.Template with the given data.
func (wr *Writer) Template(status int, tmpl *template.Template, data any) {
	wr.w.Header().Set("Content-Type", MIMEHtml)
	wr.w.WriteHeader(status)
	if err := tmpl.Execute(wr.w, data); err != nil {
		http.Error(wr.w, "template render error", http.StatusInternalServerError)
	}
}

// ----- Content negotiation -----

// Negotiate picks the response format based on the Accept header:
// protobuf → xml → json (default).
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

// ----- Shortcuts -----

// NoContent writes a 204 No Content response.
func (wr *Writer) NoContent() { wr.w.WriteHeader(http.StatusNoContent) }

// NotFound writes a 404 JSON error response.
func (wr *Writer) NotFound() { wr.JSONErr(ErrNotFound) }

// Forbidden writes a 403 JSON error response.
func (wr *Writer) Forbidden() { wr.JSONErr(ErrForbidden) }

// Unauthorized writes a 401 JSON error response.
func (wr *Writer) Unauthorized() { wr.JSONErr(ErrUnauthorized) }

// Text writes a plain-text response.
func (wr *Writer) Text(status int, text string) {
	wr.w.Header().Set("Content-Type", MIMEText)
	wr.w.WriteHeader(status)
	wr.w.Write([]byte(text))
}

// -------------------------------------------------------
// Error types
// -------------------------------------------------------

// HTTPError is an error that carries an HTTP status code and message.
type HTTPError struct {
	Status  int
	Message string
	Err     error
}

func (e *HTTPError) Error() string { return e.Message }

func (e *HTTPError) Unwrap() error { return e.Err }

// Predefined HTTP errors.
var (
	ErrBadRequest   = &HTTPError{Status: 400, Message: "Bad Request"}
	ErrUnauthorized = &HTTPError{Status: 401, Message: "Unauthorized"}
	ErrForbidden    = &HTTPError{Status: 403, Message: "Forbidden"}
	ErrNotFound     = &HTTPError{Status: 404, Message: "Not Found"}
	ErrConflict     = &HTTPError{Status: 409, Message: "Conflict"}
	ErrInternal     = &HTTPError{Status: 500, Message: "Internal Server Error"}
)

// NewError creates a custom HTTPError.
func NewError(status int, message string) *HTTPError {
	return &HTTPError{Status: status, Message: message}
}

// WrapError wraps an underlying error without exposing it to the client.
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

// -------------------------------------------------------
// Template registry
// -------------------------------------------------------

// TemplateRegistry is a thread-safe store of named html/template.Templates.
type TemplateRegistry struct {
	mu        sync.RWMutex
	templates map[string]*template.Template
}

// Templates is the global template registry.
var Templates = &TemplateRegistry{
	templates: make(map[string]*template.Template),
}

// Register stores tmpl under name.
func (tr *TemplateRegistry) Register(name string, tmpl *template.Template) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	tr.templates[name] = tmpl
}

// Get retrieves a template by name.
func (tr *TemplateRegistry) Get(name string) (*template.Template, bool) {
	tr.mu.RLock()
	defer tr.mu.RUnlock()
	t, ok := tr.templates[name]
	return t, ok
}

// RenderTemplate renders a template from the global registry by name.
func (wr *Writer) RenderTemplate(status int, name string, data any) {
	tmpl, ok := Templates.Get(name)
	if !ok {
		wr.JSONErr(NewError(500, "template not found: "+name))
		return
	}
	wr.Template(status, tmpl, data)
}

// -------------------------------------------------------
// Internal helpers
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

// traceIDFromCtx extracts a trace ID from the request context (set by tracing middleware).
func traceIDFromCtx(r *http.Request) string {
	if id, ok := r.Context().Value(ctxKeyTraceID{}).(string); ok {
		return id
	}
	return ""
}

type ctxKeyTraceID struct{}

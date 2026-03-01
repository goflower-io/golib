package app_test

import (
	"encoding/json"
	"html/template"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/goflower-io/golib/net/app"
)

// -------------------------------------------------------
// Helpers
// -------------------------------------------------------

// newWriter creates a Writer backed by a fresh recorder.
func newWriter(t *testing.T) (*app.Writer, *httptest.ResponseRecorder) {
	t.Helper()
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	return app.NewWriter(w, r), w
}

// decodeResp unmarshals the JSON body into an app.Response.
func decodeResp(t *testing.T, body string) app.Response {
	t.Helper()
	var resp app.Response
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("decode JSON: %v\nbody: %s", err, body)
	}
	return resp
}

// xmlPayload is a named struct used for XML encoding tests.
// (encoding/xml cannot encode map[string]any)
type xmlPayload struct {
	Name  string `xml:"name"`
	Value int    `xml:"value"`
}

// -------------------------------------------------------
// JSON
// -------------------------------------------------------

func TestWriter_JSON(t *testing.T) {
	wr, w := newWriter(t)
	wr.JSON(http.StatusCreated, map[string]string{"k": "v"})

	if w.Code != 201 {
		t.Fatalf("expected 201, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("unexpected Content-Type: %q", ct)
	}
	resp := decodeResp(t, w.Body.String())
	if resp.Code != 201 {
		t.Fatalf("resp.Code: expected 201, got %d", resp.Code)
	}
}

func TestWriter_JSONOk(t *testing.T) {
	wr, w := newWriter(t)
	wr.JSONOk("hello")

	resp := decodeResp(t, w.Body.String())
	if resp.Code != 200 {
		t.Fatalf("expected code 200, got %d", resp.Code)
	}
	if resp.Message != "OK" {
		t.Fatalf("expected message OK, got %q", resp.Message)
	}
}

func TestWriter_JSONErr_HTTPError(t *testing.T) {
	wr, w := newWriter(t)
	wr.JSONErr(app.ErrForbidden)

	if w.Code != 403 {
		t.Fatalf("expected 403, got %d", w.Code)
	}
	resp := decodeResp(t, w.Body.String())
	if resp.Code != 403 {
		t.Fatalf("resp.Code: expected 403, got %d", resp.Code)
	}
	if resp.Message != "Forbidden" {
		t.Fatalf("resp.Message: expected Forbidden, got %q", resp.Message)
	}
}

func TestWriter_JSONErr_GenericError(t *testing.T) {
	wr, w := newWriter(t)
	wr.JSONErr(errGeneric("something went wrong"))

	if w.Code != 500 {
		t.Fatalf("expected 500, got %d", w.Code)
	}
	resp := decodeResp(t, w.Body.String())
	if resp.Code != 500 {
		t.Fatalf("resp.Code: expected 500, got %d", resp.Code)
	}
}

// errGeneric is a plain error (not HTTPError).
type errGeneric string

func (e errGeneric) Error() string { return string(e) }

// -------------------------------------------------------
// XML
// -------------------------------------------------------

func TestWriter_XML(t *testing.T) {
	wr, w := newWriter(t)
	wr.XML(http.StatusOK, xmlPayload{Name: "test", Value: 42})

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/xml") {
		t.Fatalf("unexpected Content-Type: %q", ct)
	}
	body := w.Body.String()
	if !strings.Contains(body, "<name>test</name>") {
		t.Fatalf("XML body missing <name>test</name>: %s", body)
	}
	if !strings.Contains(body, "<value>42</value>") {
		t.Fatalf("XML body missing <value>42</value>: %s", body)
	}
}

func TestWriter_XMLOk(t *testing.T) {
	wr, w := newWriter(t)
	wr.XMLOk(xmlPayload{Name: "ok", Value: 1})

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "<code>200</code>") {
		t.Fatalf("XML missing <code>200</code>: %s", body)
	}
}

func TestWriter_XMLErr(t *testing.T) {
	wr, w := newWriter(t)
	wr.XMLErr(app.ErrNotFound)

	if w.Code != 404 {
		t.Fatalf("expected 404, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "<code>404</code>") {
		t.Fatalf("XML missing <code>404</code>: %s", body)
	}
}

// -------------------------------------------------------
// Text / HTML
// -------------------------------------------------------

func TestWriter_Text(t *testing.T) {
	wr, w := newWriter(t)
	wr.Text(http.StatusOK, "hello plain")

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "text/plain") {
		t.Fatalf("unexpected Content-Type: %q", ct)
	}
	if w.Body.String() != "hello plain" {
		t.Fatalf("unexpected body: %q", w.Body.String())
	}
}

func TestWriter_HTML(t *testing.T) {
	wr, w := newWriter(t)
	wr.HTML(http.StatusOK, "<h1>hi</h1>")

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Fatalf("unexpected Content-Type: %q", ct)
	}
	if w.Body.String() != "<h1>hi</h1>" {
		t.Fatalf("unexpected body: %q", w.Body.String())
	}
}

// -------------------------------------------------------
// Status shortcuts
// -------------------------------------------------------

func TestWriter_NoContent(t *testing.T) {
	wr, w := newWriter(t)
	wr.NoContent()

	if w.Code != 204 {
		t.Fatalf("expected 204, got %d", w.Code)
	}
	if w.Body.Len() != 0 {
		t.Fatalf("expected empty body, got %q", w.Body.String())
	}
}

func TestWriter_NotFound(t *testing.T) {
	wr, w := newWriter(t)
	wr.NotFound()
	if w.Code != 404 {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestWriter_Forbidden(t *testing.T) {
	wr, w := newWriter(t)
	wr.Forbidden()
	if w.Code != 403 {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestWriter_Unauthorized(t *testing.T) {
	wr, w := newWriter(t)
	wr.Unauthorized()
	if w.Code != 401 {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

// -------------------------------------------------------
// Content negotiation
// -------------------------------------------------------

func TestWriter_Negotiate_DefaultJSON(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	// No Accept header → default to JSON
	wr := app.NewWriter(w, r)
	wr.Negotiate(http.StatusOK, map[string]string{"k": "v"}, nil)

	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("expected JSON, got %q", ct)
	}
}

func TestWriter_Negotiate_XML(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Accept", "application/xml")
	wr := app.NewWriter(w, r)
	wr.Negotiate(http.StatusOK, xmlPayload{Name: "n", Value: 1}, nil)

	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/xml") {
		t.Fatalf("expected XML, got %q", ct)
	}
}

// -------------------------------------------------------
// Template
// -------------------------------------------------------

func TestWriter_Template(t *testing.T) {
	tmpl := template.Must(template.New("t").Parse("<p>{{.}}</p>"))
	wr, w := newWriter(t)
	wr.Template(http.StatusOK, tmpl, "world")

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "<p>world</p>") {
		t.Fatalf("unexpected body: %q", w.Body.String())
	}
}

func TestWriter_RenderTemplate(t *testing.T) {
	tmpl := template.Must(template.New("reg-tmpl").Parse("<b>{{.}}</b>"))
	app.Templates.Register("reg-tmpl", tmpl)

	wr, w := newWriter(t)
	wr.RenderTemplate(http.StatusOK, "reg-tmpl", "test")

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "<b>test</b>") {
		t.Fatalf("unexpected body: %q", w.Body.String())
	}
}

func TestWriter_RenderTemplate_NotFound(t *testing.T) {
	wr, w := newWriter(t)
	wr.RenderTemplate(http.StatusOK, "no-such-template", nil)

	if w.Code != 500 {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

// -------------------------------------------------------
// HTTPError / NewError / WrapError
// -------------------------------------------------------

func TestHTTPError_Error(t *testing.T) {
	e := app.NewError(409, "Conflict")
	if e.Error() != "Conflict" {
		t.Fatalf("expected Conflict, got %q", e.Error())
	}
}

func TestHTTPError_Unwrap(t *testing.T) {
	cause := errGeneric("db error")
	wrapped := app.WrapError(app.ErrInternal, cause)

	if wrapped.Unwrap() != cause {
		t.Fatal("Unwrap did not return the cause")
	}
	// client-visible message must not expose the cause
	if wrapped.Message != "Internal Server Error" {
		t.Fatalf("unexpected Message: %q", wrapped.Message)
	}
}

func TestNewError(t *testing.T) {
	e := app.NewError(418, "I'm a teapot")
	if e.Status != 418 {
		t.Fatalf("expected Status 418, got %d", e.Status)
	}
	if e.Message != "I'm a teapot" {
		t.Fatalf("unexpected Message: %q", e.Message)
	}
}

func TestWrapError_PreservesStatus(t *testing.T) {
	wrapped := app.WrapError(app.ErrNotFound, errGeneric("cause"))
	if wrapped.Status != 404 {
		t.Fatalf("expected Status 404, got %d", wrapped.Status)
	}
}

// -------------------------------------------------------
// Predefined errors write correct status codes
// -------------------------------------------------------

func TestPredefinedErrors(t *testing.T) {
	cases := []struct {
		err  error
		code int
	}{
		{app.ErrBadRequest, 400},
		{app.ErrUnauthorized, 401},
		{app.ErrForbidden, 403},
		{app.ErrNotFound, 404},
		{app.ErrConflict, 409},
		{app.ErrInternal, 500},
	}
	for _, c := range cases {
		wr, w := newWriter(t)
		wr.JSONErr(c.err)
		if w.Code != c.code {
			t.Errorf("%v: expected %d, got %d", c.err, c.code, w.Code)
		}
	}
}

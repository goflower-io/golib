package app

import (
	"bytes"
	"context"
	"net/http"

	"github.com/a-h/templ"
)

// Templ renders a templ.Component and streams it to the ResponseWriter.
func (wr *Writer) Templ(status int, component templ.Component) {
	wr.w.Header().Set("Content-Type", MIMEHtml)
	wr.w.WriteHeader(status)
	if err := component.Render(wr.r.Context(), wr.w); err != nil {
		// Headers are already sent; we can only log, not change the status code.
		http.Error(wr.w, "templ render error: "+err.Error(), http.StatusInternalServerError)
	}
}

// TemplOk renders a templ.Component with a 200 status code.
func (wr *Writer) TemplOk(component templ.Component) {
	wr.Templ(http.StatusOK, component)
}

// TemplErr renders an error page component with the status derived from err.
// errComponent is typically a project-level ErrorPage(code, msg) component.
func (wr *Writer) TemplErr(err error, errComponent templ.Component) {
	he := toHTTPError(err)
	wr.Templ(he.Status, errComponent)
}

// TemplFragment renders a templ.Component as an HTMX partial fragment (200).
func (wr *Writer) TemplFragment(component templ.Component) {
	wr.Templ(http.StatusOK, component)
}

// TemplString renders a templ.Component to a string.
// Useful for emails, SSE payloads, and tests.
func TemplString(ctx context.Context, component templ.Component) (string, error) {
	var buf bytes.Buffer
	if err := component.Render(ctx, &buf); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// TemplBytes renders a templ.Component to a byte slice.
func TemplBytes(ctx context.Context, component templ.Component) ([]byte, error) {
	var buf bytes.Buffer
	if err := component.Render(ctx, &buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

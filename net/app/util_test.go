package app_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/goflower-io/golib/net/app"
)

// -------------------------------------------------------
// CaculatePaginator
// -------------------------------------------------------

func TestCaculatePaginator(t *testing.T) {
	cases := []struct {
		name      string
		page      int32
		size      int32
		total     int32
		wantPage  int32 // CurrentPage
		wantTotal int32 // TotalPage
		wantPre   int32
		wantNext  int32
		wantLen   int   // len(Pages)
	}{
		{
			name:      "first page of 10",
			page:      1, size: 10, total: 100,
			wantPage: 1, wantTotal: 10, wantPre: 1, wantNext: 2, wantLen: 5,
		},
		{
			name:      "middle page",
			page:      5, size: 10, total: 100,
			wantPage: 5, wantTotal: 10, wantPre: 4, wantNext: 6, wantLen: 5,
		},
		{
			name:      "last page clamped",
			page:      10, size: 10, total: 100,
			wantPage: 10, wantTotal: 10, wantPre: 9, wantNext: 10, wantLen: 5,
		},
		{
			name:      "page 0 becomes 1",
			page:      0, size: 10, total: 100,
			wantPage: 1, wantTotal: 10, wantPre: 1, wantNext: 2, wantLen: 5,
		},
		{
			name:      "page > total clamped",
			page:      99, size: 10, total: 100,
			wantPage: 10, wantTotal: 10, wantPre: 9, wantNext: 10, wantLen: 5,
		},
		{
			name:      "fewer than 5 pages",
			page:      2, size: 10, total: 30,
			wantPage: 2, wantTotal: 3, wantPre: 1, wantNext: 3, wantLen: 3,
		},
		{
			name:      "single page",
			page:      1, size: 10, total: 5,
			wantPage: 1, wantTotal: 1, wantPre: 1, wantNext: 2, wantLen: 1,
		},
		{
			name:      "zero total",
			page:      1, size: 10, total: 0,
			wantPage: 1, wantTotal: 0, wantPre: 1, wantNext: 2, wantLen: 0,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := app.CaculatePaginator(c.page, c.size, c.total)
			if p.CurrentPage != c.wantPage {
				t.Errorf("CurrentPage: want %d, got %d", c.wantPage, p.CurrentPage)
			}
			if p.TotalPage != c.wantTotal {
				t.Errorf("TotalPage: want %d, got %d", c.wantTotal, p.TotalPage)
			}
			if p.Pre != c.wantPre {
				t.Errorf("Pre: want %d, got %d", c.wantPre, p.Pre)
			}
			if p.Next != c.wantNext {
				t.Errorf("Next: want %d, got %d", c.wantNext, p.Next)
			}
			if len(p.Pages) != c.wantLen {
				t.Errorf("len(Pages): want %d, got %d (pages=%v)", c.wantLen, len(p.Pages), p.Pages)
			}
		})
	}
}

// Pages slice must always be sorted ascending.
func TestCaculatePaginator_PagesAscending(t *testing.T) {
	p := app.CaculatePaginator(5, 10, 100)
	for i := 1; i < len(p.Pages); i++ {
		if p.Pages[i] <= p.Pages[i-1] {
			t.Fatalf("Pages not ascending: %v", p.Pages)
		}
	}
}

// -------------------------------------------------------
// GetRequestParams
// -------------------------------------------------------

type testParams struct {
	Name  string `json:"name"  form:"name"`
	Email string `json:"email" form:"email"`
}

func TestGetRequestParams_JSON(t *testing.T) {
	body := `{"name":"Alice","email":"alice@example.com"}`
	r := httptest.NewRequest("POST", "/", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")

	var p testParams
	if err := app.GetRequestParams(&p, r); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Name != "Alice" {
		t.Errorf("Name: want Alice, got %q", p.Name)
	}
	if p.Email != "alice@example.com" {
		t.Errorf("Email: want alice@example.com, got %q", p.Email)
	}
}

func TestGetRequestParams_Form(t *testing.T) {
	r := httptest.NewRequest("POST", "/", strings.NewReader("name=Bob&email=bob%40example.com"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	var p testParams
	if err := app.GetRequestParams(&p, r); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Name != "Bob" {
		t.Errorf("Name: want Bob, got %q", p.Name)
	}
	if p.Email != "bob@example.com" {
		t.Errorf("Email: want bob@example.com, got %q", p.Email)
	}
}

func TestGetRequestParams_InvalidJSON(t *testing.T) {
	r := httptest.NewRequest("POST", "/", strings.NewReader("{bad json"))
	r.Header.Set("Content-Type", "application/json")

	var p testParams
	if err := app.GetRequestParams(&p, r); err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

// -------------------------------------------------------
// ResponseConentType
// -------------------------------------------------------

func TestResponseConentType(t *testing.T) {
	cases := []struct {
		name    string
		headers map[string]string
		want    app.ResponseType
	}{
		{
			name:    "JSON via Accept",
			headers: map[string]string{"Accept": "application/json"},
			want:    app.ResponseJSON,
		},
		{
			name:    "HTMX via HX-Request",
			headers: map[string]string{"HX-Request": "true"},
			want:    app.ResponseHTMX,
		},
		{
			name:    "default HTML",
			headers: map[string]string{},
			want:    app.ResponseHTML,
		},
		{
			name:    "JSON takes priority over HTMX",
			headers: map[string]string{"Accept": "application/json", "HX-Request": "true"},
			want:    app.ResponseJSON,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/", nil)
			for k, v := range c.headers {
				r.Header.Set(k, v)
			}
			got := app.ResponseConentType(r)
			if got != c.want {
				t.Errorf("want %q, got %q", c.want, got)
			}
		})
	}
}

// -------------------------------------------------------
// Validator singleton
// -------------------------------------------------------

func TestValidator_Singleton(t *testing.T) {
	v1 := app.Validator()
	v2 := app.Validator()
	if v1 != v2 {
		t.Fatal("Validator() must return the same instance")
	}
}

func TestValidator_RequiredField(t *testing.T) {
	type req struct {
		Name string `validate:"required"`
	}
	v := app.Validator()
	if err := v.Struct(req{}); err == nil {
		t.Fatal("expected validation error for empty required field")
	}
	if err := v.Struct(req{Name: "ok"}); err != nil {
		t.Fatalf("unexpected error for valid struct: %v", err)
	}
}

// -------------------------------------------------------
// MIME constants are non-empty
// -------------------------------------------------------

func TestMIMEConstants(t *testing.T) {
	mimes := []string{
		app.MIMEJson, app.MIMEXml, app.MIMEProtobuf, app.MIMEHtml, app.MIMEText,
	}
	for _, m := range mimes {
		if m == "" {
			t.Errorf("MIME constant is empty")
		}
	}
}

// -------------------------------------------------------
// App — HTTP handler chain (integration, no gRPC port needed)
// -------------------------------------------------------

func TestApp_HTTPRoutes(t *testing.T) {
	a := app.New(app.WithAddr("127.0.0.1", 19999))

	a.GET("/ping", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("pong"))
	})
	a.POST("/echo", app.H(func(w *app.Writer, r *http.Request) {
		w.JSONOk("echoed")
	}))

	// Use the embedded Router directly (no need to bind a real port).
	srv := httptest.NewServer(a.Router)
	defer srv.Close()

	// GET /ping
	resp, err := http.Get(srv.URL + "/ping")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("GET /ping: expected 200, got %d", resp.StatusCode)
	}

	// POST /echo
	resp2, err := http.Post(srv.URL+"/echo", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != 200 {
		t.Fatalf("POST /echo: expected 200, got %d", resp2.StatusCode)
	}
}

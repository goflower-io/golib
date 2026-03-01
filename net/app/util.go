package app

import (
	"encoding/json"
	"io"
	"math"
	"net/http"
	"sync"

	form "github.com/go-playground/form/v4"
	"github.com/go-playground/validator/v10"
)

// Validator is a lazily-initialized struct validator.
var Validator = sync.OnceValue(func() *validator.Validate {
	return validator.New(validator.WithRequiredStructEnabled())
})

// FormParser is a lazily-initialized form decoder.
var FormParser = sync.OnceValue(func() *form.Decoder {
	return form.NewDecoder()
})

// ResponseType indicates which response format a handler should use.
type ResponseType string

const (
	ResponseJSON ResponseType = "json"
	ResponseHTML ResponseType = "html"
	ResponseHTMX ResponseType = "htmx"
)

// ResponseConentType infers the desired response format from request headers.
func ResponseConentType(req *http.Request) ResponseType {
	if req.Header.Get("Accept") == "application/json" {
		return ResponseJSON
	}
	if req.Header.Get("HX-Request") == "true" {
		return ResponseHTMX
	}
	return ResponseHTML
}

// GetRequestParams decodes the request body into reqb.
// JSON bodies are decoded by Content-Type; everything else is treated as form data.
func GetRequestParams(reqb any, req *http.Request) error {
	contentType := req.Header.Get("Content-Type")
	switch contentType {
	case "application/json":
		body, err := io.ReadAll(req.Body)
		if err != nil {
			return err
		}
		if err = json.Unmarshal(body, reqb); err != nil {
			return err
		}
	default:
		// application/x-www-form-urlencoded or multipart/form-data
		if err := req.ParseForm(); err != nil {
			return err
		}
		if err := FormParser().Decode(reqb, req.Form); err != nil {
			return err
		}
	}
	return nil
}

// CaculatePaginator computes a paginator for the given page, page size, and
// total record count. It returns up to 5 visible page numbers in the style:
// ← 1 2 3 4 5 →
func CaculatePaginator(page, size, total int32) *Paginator {
	var pre, next int32
	totalPage := int32(math.Ceil(float64(total) / float64(size)))
	if page > totalPage {
		page = totalPage
	}
	if page <= 0 {
		page = 1
	}
	var pages []int32
	switch {
	case page >= totalPage-5 && totalPage > 5: // last 5 pages
		start := totalPage - 5 + 1
		pre = page - 1
		next = int32(math.Min(float64(totalPage), float64(page+1)))
		pages = make([]int32, 5)
		for i := range pages {
			pages[i] = start + int32(i)
		}
	case page >= 3 && totalPage > 5:
		start := page - 3 + 1
		pages = make([]int32, 5)
		for i := range pages {
			pages[i] = start + int32(i)
		}
		pre = page - 1
		next = page + 1
	default:
		pages = make([]int32, int32(math.Min(5, float64(totalPage))))
		for i := range pages {
			pages[i] = int32(i) + 1
		}
		pre = int32(math.Max(float64(1), float64(page-1)))
		next = page + 1
	}
	return &Paginator{
		Pages:       pages,
		TotalPage:   totalPage,
		Pre:         pre,
		Next:        next,
		CurrentPage: page,
		PageSize:    size,
	}
}

// Paginator holds the computed pagination state for a single page view.
type Paginator struct {
	Pages       []int32
	TotalPage   int32
	Pre         int32
	Next        int32
	CurrentPage int32
	PageSize    int32
}

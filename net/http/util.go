package http

import (
	"encoding/json"
	"io"
	"math"
	"net/http"
	"sync"

	form "github.com/go-playground/form/v4"
)

var FormParser = sync.OnceValue(func() *form.Decoder {
	return form.NewDecoder()
})

func ResponseConentType(req *http.Request) ResponseType {
	if req.Header.Get("Accept") == "application/json" {
		return ResponseJSON
	}
	if req.Header.Get("HX-Request") == "true" {
		return ResponseHTMX
	}
	return ResponseHTML
}

type ResponseType string

const (
	ResponseJSON ResponseType = "json"
	ResponseHTML ResponseType = "html"
	ResponseHTMX ResponseType = "htmx"
)

func GetRequestParams(reqb any, req *http.Request) error {
	contentType := req.Header.Get("Content-Type")
	switch contentType {
	case "application/json":
		var body []byte
		body, err := io.ReadAll(req.Body)
		if err != nil {
			return err
		}

		if err = json.Unmarshal(body, reqb); err != nil {
			return err
		}
	default:
		// "application/x-www-form-urlencoded":
		if err := req.ParseForm(); err != nil {
			return err
		}
		if err := FormParser().Decode(reqb, req.Form); err != nil {
			return err
		}
	}
	return nil
}

// 分页方法，根据传递过来的页数，每页数，总数，返回分页的内容 7个页数 前 1，2，3，4，5 后 的格式返回,小于5页返回具体页数
func CaculatePaginator(page, size int, total int64) *Paginator {
	var pre int  // 前一页地址
	var next int // 后一页地址
	// 根据nums总数，和prepage每页数量 生成分页总数
	totalPage := int(math.Ceil(float64(total) / float64(size))) // page总数
	if page > totalPage {
		page = totalPage
	}
	if page <= 0 {
		page = 1
	}
	var pages []int
	switch {
	case page >= totalPage-5 && totalPage > 5: // 最后5页
		start := totalPage - 5 + 1
		pre = page - 1
		next = int(math.Min(float64(totalPage), float64(page+1)))
		pages = make([]int, 5)
		for i := range pages {
			pages[i] = start + i
		}
	case page >= 3 && totalPage > 5:
		start := page - 3 + 1
		pages = make([]int, 5)
		for i := range pages {
			pages[i] = start + i
		}
		pre = page - 1
		next = page + 1
	default:
		pages = make([]int, int(math.Min(5, float64(totalPage))))
		for i := range pages {
			pages[i] = i + 1
		}
		pre = int(math.Max(float64(1), float64(page-1)))
		next = page + 1
	}
	paginator := &Paginator{}
	paginator.Pages = pages
	paginator.TotalPage = totalPage
	paginator.Pre = pre
	paginator.Next = next
	paginator.CurrentPage = page
	paginator.PageSize = size
	return paginator
}

type Paginator struct {
	Pages       []int
	TotalPage   int
	Pre         int
	Next        int
	CurrentPage int
	PageSize    int
}

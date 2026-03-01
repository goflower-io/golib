package app

import (
	"bytes"
	"context"
	"net/http"

	"github.com/a-h/templ"
)

// Templ 渲染 templ.Component，直接流式写入 ResponseWriter
func (wr *Writer) Templ(status int, component templ.Component) {
	wr.w.Header().Set("Content-Type", MIMEHtml)
	wr.w.WriteHeader(status)
	if err := component.Render(wr.r.Context(), wr.w); err != nil {
		// header 已发出，只能记录日志，无法再改状态码
		// 生产建议接入 slog/zap
		http.Error(wr.w, "templ render error: "+err.Error(), http.StatusInternalServerError)
	}
}

// TemplOk 200 渲染 templ 组件（最常用）
func (wr *Writer) TemplOk(component templ.Component) {
	wr.Templ(http.StatusOK, component)
}

// TemplErr 渲染 templ 错误页组件
// errComponent 通常是你项目里定义好的 ErrorPage(code, msg) templ.Component
func (wr *Writer) TemplErr(err error, errComponent templ.Component) {
	he := toHTTPError(err)
	wr.Templ(he.Status, errComponent)
}

// TemplFragment 渲染 templ 组件作为 HTMX 局部片段
// 自动配合已设置的 HX-* 头，无需额外操作
func (wr *Writer) TemplFragment(component templ.Component) {
	wr.Templ(http.StatusOK, component)
}

// TemplString 将 templ 组件渲染为字符串（用于邮件、SSE、测试等非 HTTP 场景）
func TemplString(ctx context.Context, component templ.Component) (string, error) {
	var buf bytes.Buffer
	if err := component.Render(ctx, &buf); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// TemplBytes 将 templ 组件渲染为 []byte
func TemplBytes(ctx context.Context, component templ.Component) ([]byte, error) {
	var buf bytes.Buffer
	if err := component.Render(ctx, &buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

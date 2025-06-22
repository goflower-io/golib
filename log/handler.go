package log

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/goflower-io/golib/log/buffer"
)

type Record struct {
	time    time.Time
	level   slog.Level
	source  string
	gid     int64
	message string
	attrs   []*slog.Attr
}

func (r *Record) AddAttrs(attrs ...*slog.Attr) {
	if len(attrs) > 0 {
		r.attrs = append(r.attrs, attrs...)
	}
}

func (r *Record) Attrs(f func(a *slog.Attr) bool) {
	for _, v := range r.attrs {
		if v != nil {
			f(v)
		}
	}
}

type vhandler struct {
	lv   *slog.LevelVar
	w    io.Writer
	name string
}

func (h *vhandler) enabled(ctx context.Context, l slog.Level) bool {
	minLevel := slog.LevelInfo
	if h.lv != nil {
		minLevel = h.lv.Level()
	}
	return l >= minLevel
}

func (h *vhandler) handle(ctx context.Context, r *Record) error {
	buf := buffer.New()
	defer buf.Free()
	bracket(buf, r.time.Format("2006-01-02 15:04:05.000"))
	bracket(buf, r.level.String())
	bracket(buf, h.name)
	bracket(buf, r.source)
	bracket(buf, fmt.Sprintf("gid=%d", r.gid))

	r.Attrs(func(a *slog.Attr) bool {
		bracket(buf, fmt.Sprintf("%s=%s", a.Key, a.Value.String()))
		return true
	})
	buf.WriteString(r.message)
	buf.WriteByte('\n')
	h.w.Write(*buf)
	return nil
}

func bracket(buf *buffer.Buffer, val string) {
	buf.WriteString("[")
	buf.WriteString(val)
	buf.WriteString("] ")
}

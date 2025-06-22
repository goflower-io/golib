package log

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"time"

	"github.com/petermattis/goid"

	"github.com/goflower-io/golib/log/rotate"
)

// ExtractAttrFn extract Attr form Context
type ExtractAttrFn func(context.Context) []*slog.Attr

// Logger Logger hold writer level  extractFn
type Logger struct {
	handler                    *vhandler
	levelVar                   *slog.LevelVar
	extractAttrsFromContextFns []ExtractAttrFn
}

// New create a *Logger if rootpath == "" output log to console stdout
func New(rootpath string, olv slog.Level) (*Logger, error) {
	exec, err := os.Executable()
	if err != nil {
		return nil, err
	}
	prefix := path.Base(exec)
	var out io.Writer
	if rootpath != "" {
		out, err = rotate.New(rootpath, prefix)
		if err != nil {
			return nil, err
		}
	} else {
		// if rootpath =="" mean log to console
		out = os.Stdout
	}
	lv := &slog.LevelVar{}
	lv.Set(olv.Level())

	return &Logger{
		levelVar: lv,
		handler: &vhandler{
			w:    out,
			lv:   lv,
			name: prefix,
		},
	}, nil
}

// SetLevel set Logger level
func (l *Logger) SetLevel(level slog.Level) {
	l.levelVar.Set(level)
}

// SetMaxFileSize max uint:byte
func (l *Logger) SetMaxFileSize(max int) {
	if r, ok := l.handler.w.(*rotate.Writer); ok {
		r.SetMax(max)
	}
}

// AddExtractAttrsFromContextFn add Fn that can get key and value from context
func (l *Logger) AddExtractAttrsFromContextFn(fn ExtractAttrFn) {
	l.extractAttrsFromContextFns = append(l.extractAttrsFromContextFns, fn)
}

// Close  close underlying file
func (l *Logger) Close() {
	if r, ok := l.handler.w.(*rotate.Writer); ok {
		r.Close()
	}
}

func (l *Logger) logf(ctx context.Context, level slog.Level, format string, args ...any) {
	if !l.handler.enabled(ctx, level) {
		return
	}
	_, file, line, _ := runtime.Caller(2) // skip [logf,logfcaller]
	r := &Record{
		time:    time.Now(),
		level:   level,
		source:  fmt.Sprintf("%s:%d", filepath.Base(file), line),
		gid:     goid.Get(),
		message: fmt.Sprintf(format, args...),
	}
	if ctx != nil {
		for _, fn := range l.extractAttrsFromContextFns {
			attrs := fn(ctx)
			if len(attrs) > 0 {
				r.AddAttrs(attrs...)
			}
		}
	}
	_ = l.handler.handle(ctx, r)
}

func (l *Logger) log(ctx context.Context, level slog.Level, args ...any) {
	if !l.handler.enabled(ctx, level) {
		return
	}
	_, file, line, _ := runtime.Caller(2) // skip [ logf,logfcaller]
	r := &Record{
		time:    time.Now(),
		level:   level,
		source:  fmt.Sprintf("%s:%d", filepath.Base(file), line),
		gid:     goid.Get(),
		message: fmt.Sprint(args...),
	}
	if ctx != nil {
		for _, fn := range l.extractAttrsFromContextFns {
			attrs := fn(ctx)
			if len(attrs) > 0 {
				r.AddAttrs(attrs...)
			}
		}
	}
	_ = l.handler.handle(ctx, r)
}

// Debug level
func (l *Logger) Debug(args ...any) {
	l.log(context.Background(), slog.LevelDebug, args...)
}

func (l *Logger) Info(args ...any) {
	l.log(context.Background(), slog.LevelInfo, args...)
}

func (l *Logger) Warn(args ...any) {
	l.log(context.Background(), slog.LevelWarn, args...)
}

func (l *Logger) Error(args ...any) {
	l.log(context.Background(), slog.LevelError, args...)
}

func (l *Logger) DebugContext(ctx context.Context, args ...any) {
	l.log(ctx, slog.LevelDebug, args...)
}

func (l *Logger) InfoContext(ctx context.Context, args ...any) {
	l.log(ctx, slog.LevelInfo, args...)
}

func (l *Logger) WarnContext(ctx context.Context, args ...any) {
	l.log(ctx, slog.LevelWarn, args...)
}

func (l *Logger) ErrorContext(ctx context.Context, args ...any) {
	l.log(ctx, slog.LevelError, args...)
}

func (l *Logger) Debugf(format string, args ...any) {
	l.logf(context.Background(), slog.LevelDebug, format, args...)
}

func (l *Logger) Infof(format string, args ...any) {
	l.logf(context.Background(), slog.LevelInfo, format, args...)
}

func (l *Logger) Warnf(format string, args ...any) {
	l.logf(context.Background(), slog.LevelWarn, format, args...)
}

func (l *Logger) Errorf(format string, args ...any) {
	l.logf(context.Background(), slog.LevelError, format, args...)
}

func (l *Logger) DebugfContext(ctx context.Context, format string, args ...any) {
	l.logf(ctx, slog.LevelDebug, format, args...)
}

func (l *Logger) InfofContext(ctx context.Context, format string, args ...any) {
	l.logf(ctx, slog.LevelInfo, format, args...)
}

func (l *Logger) WarnfContext(ctx context.Context, format string, args ...any) {
	l.logf(ctx, slog.LevelWarn, format, args...)
}

func (l *Logger) ErrorfContext(ctx context.Context, format string, args ...any) {
	l.logf(ctx, slog.LevelError, format, args...)
}

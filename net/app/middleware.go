package app

import (
	"errors"
	"log/slog"
	"net/http"
	"runtime"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// RecoveryMiddle catches panics, logs the stack trace, and returns 500.
var RecoveryMiddle = func(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				stack := make([]byte, 1024*8)
				stack = stack[:runtime.Stack(stack, false)]
				slog.ErrorContext(r.Context(), "panic",
					slog.String("path", r.URL.Path),
					slog.Any("error", err),
					slog.Any("stack", stack),
				)
			}
		}()
		h.ServeHTTP(w, r)
	}
}

// LogMidddle logs method, path, status, and duration for every request.
var LogMidddle = func(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		re := &StatusRecorder{ResponseWriter: w}
		h.ServeHTTP(re, r)

		slog.InfoContext(
			r.Context(),
			"http request",
			slog.Int("status", re.Status),
			slog.String("duration", time.Since(start).String()),
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
		)
	}
}

// StatusRecorder wraps http.ResponseWriter to capture the written status code.
type StatusRecorder struct {
	http.ResponseWriter
	Status int
}

func (r *StatusRecorder) WriteHeader(status int) {
	r.Status = status
	r.ResponseWriter.WriteHeader(status)
}

// PromMiddleWare records request count and latency as Prometheus metrics.
type PromMiddleWare struct {
	reqs    *prometheus.CounterVec
	latency *prometheus.HistogramVec
}

// MetricMiddle creates and registers Prometheus metrics for the given service name.
// Optional buckets override the default millisecond histogram buckets.
// If the metrics are already registered (e.g. in tests), the existing collectors are reused.
func MetricMiddle(name string, buckets ...float64) *PromMiddleWare {
	var m PromMiddleWare
	m.reqs = mustRegister(prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:        "http_requests_total",
			Help:        "How many HTTP requests processed, partitioned by status code, method and HTTP path.",
			ConstLabels: prometheus.Labels{"service": name},
		},
		[]string{"code", "method", "path"},
	)).(*prometheus.CounterVec)

	if len(buckets) == 0 {
		buckets = []float64{300, 1200, 5000}
	}
	m.latency = mustRegister(prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:        "http_request_duration_milliseconds",
		Help:        "How long it took to process the request, partitioned by status code, method and HTTP path.",
		ConstLabels: prometheus.Labels{"service": name},
		Buckets:     buckets,
	},
		[]string{"code", "method", "path"},
	)).(*prometheus.HistogramVec)
	return &m
}

// mustRegister registers c and returns it. If c is already registered,
// the existing collector is returned instead of panicking.
func mustRegister(c prometheus.Collector) prometheus.Collector {
	if err := prometheus.Register(c); err != nil {
		var are prometheus.AlreadyRegisteredError
		if errors.As(err, &are) {
			return are.ExistingCollector
		}
		panic(err)
	}
	return c
}

// Hander wraps h to record Prometheus metrics for each request.
func (m *PromMiddleWare) Hander(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		re := &StatusRecorder{ResponseWriter: w}
		h.ServeHTTP(re, r)
		m.reqs.WithLabelValues(http.StatusText(re.Status), r.Method, r.URL.Path).Inc()
		m.latency.WithLabelValues(http.StatusText(re.Status), r.Method, r.URL.Path).Observe(float64(time.Since(start).Nanoseconds()) / 1000000)
	}
}

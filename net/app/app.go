package app

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/improbable-eng/grpc-web/go/grpcweb"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/cors"
	"github.com/soheilhy/cmux"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
)

// Addr holds a host:port pair.
type Addr struct {
	IP   string
	Port int
}

func (a *Addr) String() string {
	return fmt.Sprintf("%s:%d", a.IP, a.Port)
}

// TLSConfig holds the address and certificate paths for a TLS listener.
type TLSConfig struct {
	Addr
	KeyPath  string
	CertPath string
}

// SLogConfig configures the default slog handler.
type SLogConfig struct {
	Level      slog.Level
	JSONOutPut bool
	AddSource  bool
}

// AppOptions collects optional configuration for App.
type AppOptions struct {
	httptls       *TLSConfig    // nil → no TLS listener
	http          *Addr         // nil → no plain-TCP listener; same port as TLS → shared listener
	enableGRPCWeb bool
	corsOptions   *cors.Options
	prom          *Addr // nil → no Prometheus metrics server
	slog          *SLogConfig
}

// AppOption is a functional option for New.
type AppOption func(aos *AppOptions)

// WithTLSConfig enables a TLS listener using the given certificate and key.
func WithTLSConfig(c *TLSConfig) AppOption {
	return func(aos *AppOptions) { aos.httptls = c }
}

// WithAddr enables a plain-TCP HTTP/gRPC listener on ip:p.
func WithAddr(ip string, p int) AppOption {
	return func(aos *AppOptions) { aos.http = &Addr{IP: ip, Port: p} }
}

// WithPromAddr starts a separate Prometheus metrics server on ip:p.
func WithPromAddr(ip string, p int) AppOption {
	return func(aos *AppOptions) { aos.prom = &Addr{IP: ip, Port: p} }
}

// WihtGrpcWeb enables grpc-web proxying on the HTTP listener.
func WihtGrpcWeb(open bool) AppOption {
	return func(aos *AppOptions) { aos.enableGRPCWeb = open }
}

// WithCorsOptions applies CORS headers using the given options.
func WithCorsOptions(opt *cors.Options) AppOption {
	return func(aos *AppOptions) { aos.corsOptions = opt }
}

// WithSlogConfig replaces the default slog handler.
func WithSlogConfig(l *SLogConfig) AppOption {
	return func(aos *AppOptions) { aos.slog = l }
}

// App is the top-level server. It embeds *Router so all routing methods
// (GET, POST, Group, Use, …) are available directly on App.
type App struct {
	*Router
	options *AppOptions
	rpc     *grpc.Server
	h1      *http.Server
	wg      sync.WaitGroup
	mList   []cmux.CMux
	prom    *http.Server
}

// New creates an App with the given options. Health-check and reflection
// services are registered on the gRPC server automatically.
func New(options ...AppOption) *App {
	a := &App{
		Router:  NewRouter(),
		options: &AppOptions{},
		rpc:     grpc.NewServer(),
		h1:      &http.Server{},
	}
	for _, opt := range options {
		opt(a.options)
	}
	grpc_health_v1.RegisterHealthServer(a.rpc, health.NewServer())
	reflection.Register(a.rpc)
	return a
}

// go_ runs f in a tracked goroutine.
func (a *App) go_(f func()) {
	a.wg.Add(1)
	go func() { defer a.wg.Done(); f() }()
}

// serve demuxes gRPC from all other traffic on l and starts both servers.
// l may be a plain TCP or TLS listener — the logic is identical for both.
//
// Matching order (first match wins):
//  1. gRPC  — HTTP/2 with content-type: application/grpc
//  2. HTTP  — everything else (HTTP/1.x, health probes, …)
func (a *App) serve(l net.Listener) {
	slog.Info("serving", "addr", l.Addr())
	m := cmux.New(l)

	grpcL := m.MatchWithWriters(
		cmux.HTTP2MatchHeaderFieldPrefixSendSettings("content-type", "application/grpc"),
	)
	httpL := m.Match(cmux.Any()) // cmux.Any() ensures no connection is left unmatched

	a.go_(func() {
		if err := a.rpc.Serve(grpcL); err != nil {
			slog.Error("grpc stopped", "err", err)
		}
	})
	a.go_(func() {
		if err := a.h1.Serve(httpL); err != nil {
			slog.Error("http stopped", "err", err)
		}
	})
	a.go_(func() {
		if err := m.Serve(); err != nil {
			slog.Error("cmux stopped", "err", err)
		}
	})
	a.mList = append(a.mList, m)
}

// isTLS detects a TLS ClientHello by its first byte (0x16 = TLS handshake).
// Used as a cmux.Matcher to split TLS from plain-text on a shared port.
var isTLS cmux.Matcher = func(r io.Reader) bool {
	b := [1]byte{}
	_, err := r.Read(b[:])
	return err == nil && b[0] == 0x16
}

// start opens listeners and launches serving goroutines.
//
// Three modes:
//   - plain only:    one serve() call on a TCP listener
//   - TLS only:      one serve() call on a TLS listener
//   - same port:     outer cmux splits TLS from plain, then serve() for each side
//   - different port: independent serve() calls on each listener
func (a *App) start() {
	o := a.options
	if o.http == nil && o.httptls == nil {
		panic("at least one listen address is required (WithAddr or WithTLSConfig)")
	}

	if o.http != nil && o.httptls != nil && o.http.Port == o.httptls.Port {
		// Same port: a single outer cmux detects TLS by its first byte and
		// routes each side to an independent serve() call. No code duplication.
		raw := mustListen("tcp", o.http.String())
		tlsCfg := mustTLSConfig(o.httptls.CertPath, o.httptls.KeyPath)

		m := cmux.New(raw)
		tlsRaw := m.Match(isTLS)
		plainL := m.Match(cmux.Any())

		a.go_(func() {
			if err := m.Serve(); err != nil {
				slog.Error("cmux (outer) stopped", "err", err)
			}
		})
		a.mList = append(a.mList, m)

		a.serve(plainL)
		a.serve(tls.NewListener(tlsRaw, tlsCfg))
		return
	}

	if o.http != nil {
		a.serve(mustListen("tcp", o.http.String()))
	}
	if o.httptls != nil {
		tlsCfg := mustTLSConfig(o.httptls.CertPath, o.httptls.KeyPath)
		l, err := tls.Listen("tcp", o.httptls.String(), tlsCfg)
		if err != nil {
			panic(err)
		}
		a.serve(l)
	}
}

// runProm starts the Prometheus metrics server if configured.
func (a *App) runProm() {
	if a.options.prom == nil {
		return
	}
	met := http.NewServeMux()
	met.Handle("GET /metrics", promhttp.Handler())
	a.prom = &http.Server{Addr: a.options.prom.String(), Handler: met}
	a.go_(func() {
		if err := a.prom.ListenAndServe(); err != nil {
			slog.Error("metrics server stopped", "err", err)
		}
	})
}

// configDefaultSlog replaces the default slog handler if WithSlogConfig was used.
func (a *App) configDefaultSlog() {
	if a.options.slog == nil {
		return
	}
	opt := &slog.HandlerOptions{
		AddSource: a.options.slog.AddSource,
		Level:     a.options.slog.Level,
	}
	if a.options.slog.JSONOutPut {
		slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, opt)))
	} else {
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, opt)))
	}
}

// Run starts all servers and blocks until an OS signal is received,
// then performs a graceful shutdown.
func (a *App) Run() {
	a.configDefaultSlog()
	slog.Info("start server")
	a.loadH1Handler()
	a.start()
	a.runProm()

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGHUP, syscall.SIGQUIT, syscall.SIGTERM, syscall.SIGINT)
	<-ch

	// Stop accepting new connections first, then drain in-flight requests.
	for _, m := range a.mList {
		m.Close()
	}
	if a.h1 != nil {
		a.h1.Shutdown(context.Background())
	}
	if a.rpc != nil {
		a.rpc.GracefulStop()
	}
	if a.prom != nil {
		a.prom.Shutdown(context.Background())
	}
	slog.Info("finish server")
	a.wg.Wait()
}

// RegisteGrpcService registers a gRPC service implementation with the server.
func (a *App) RegisteGrpcService(desc *grpc.ServiceDesc, s any) {
	a.rpc.RegisterService(desc, s)
}

// loadH1Handler assembles the HTTP handler chain:
// Router → (optional grpc-web) → (optional CORS) → recovery/log/metrics.
func (a *App) loadH1Handler() {
	var h http.Handler = a.Router

	if a.options.enableGRPCWeb {
		wrappedGrpc := grpcweb.WrapServer(a.rpc)
		h = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if wrappedGrpc.IsGrpcWebRequest(r) {
				wrappedGrpc.ServeHTTP(w, r)
				return
			}
			a.Router.ServeHTTP(w, r)
		})
	}
	if a.options.corsOptions != nil {
		h = cors.New(*a.options.corsOptions).Handler(h)
	}
	// Wrap with recovery, structured logging, and Prometheus metrics.
	a.h1.Handler = RecoveryMiddle(LogMidddle(MetricMiddle("app").Hander(h.ServeHTTP)))
}

// -------------------------------------------------------
// Internal helpers
// -------------------------------------------------------

func mustListen(network, addr string) net.Listener {
	l, err := net.Listen(network, addr)
	if err != nil {
		panic(err)
	}
	return l
}

func mustTLSConfig(certFile, keyFile string) *tls.Config {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		panic(err)
	}
	return &tls.Config{Certificates: []tls.Certificate{cert}}
}

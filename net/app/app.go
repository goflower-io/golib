package app

import (
	"context"
	"crypto/tls"
	"fmt"
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
	httptls       *TLSConfig  // nil → no TLS listener
	http          *Addr       // nil → no plain-TCP listener; same port as TLS → shared listener
	enableGRPCWeb bool
	corsOptions   *cors.Options
	prom          *Addr // nil → no Prometheus metrics server
	slog          *SLogConfig
}

// AppOption is a functional option for New.
type AppOption func(aos *AppOptions)

// WithTLSConfig enables a TLS listener using the given certificate and key.
func WithTLSConfig(c *TLSConfig) AppOption {
	return func(aos *AppOptions) {
		aos.httptls = c
	}
}

// WithAddr enables a plain-TCP HTTP/gRPC listener on ip:p.
func WithAddr(ip string, p int) AppOption {
	return func(aos *AppOptions) {
		aos.http = &Addr{IP: ip, Port: p}
	}
}

// WithPromAddr starts a separate Prometheus metrics server on ip:p.
func WithPromAddr(ip string, p int) AppOption {
	return func(aos *AppOptions) {
		aos.prom = &Addr{IP: ip, Port: p}
	}
}

// WihtGrpcWeb enables grpc-web proxying on the HTTP listener.
func WihtGrpcWeb(open bool) AppOption {
	return func(aos *AppOptions) {
		aos.enableGRPCWeb = open
	}
}

// WithCorsOptions applies CORS headers using the given options.
func WithCorsOptions(opt *cors.Options) AppOption {
	return func(aos *AppOptions) {
		aos.corsOptions = opt
	}
}

// WithSlogConfig replaces the default slog handler.
func WithSlogConfig(l *SLogConfig) AppOption {
	return func(aos *AppOptions) {
		aos.slog = l
	}
}

// App is the top-level server. It embeds *Router so all routing methods
// (GET, POST, Group, Use, …) are available directly on App.
type App struct {
	*Router
	options    *AppOptions
	bothTCPTLS net.Listener
	onlyTCP    net.Listener
	onlyTLS    net.Listener
	rpc        *grpc.Server
	h1         *http.Server
	wg         sync.WaitGroup
	mList      []cmux.CMux
	prom       *http.Server
}

// New creates an App with the given options. Health-check and reflection
// services are registered on the gRPC server automatically.
func New(options ...AppOption) *App {
	a := &App{
		Router:  NewRouter(),
		options: &AppOptions{},
		rpc:     grpc.NewServer(),
		h1:      &http.Server{},
		wg:      sync.WaitGroup{},
	}
	for _, opt := range options {
		opt(a.options)
	}
	grpc_health_v1.RegisterHealthServer(a.rpc, health.NewServer())
	reflection.Register(a.rpc)
	return a
}

func (a *App) listenTLS() {
	cert, err := tls.LoadX509KeyPair(a.options.httptls.CertPath, a.options.httptls.KeyPath)
	if err != nil {
		panic(err)
	}
	cfg := &tls.Config{Certificates: []tls.Certificate{cert}}
	a.onlyTLS, err = tls.Listen("tcp", a.options.httptls.String(), cfg)
	if err != nil {
		panic(err)
	}
}

func (a *App) listenTCP() {
	var err error
	a.onlyTCP, err = net.Listen("tcp", a.options.http.String())
	if err != nil {
		panic(err)
	}
}

// listens opens the configured listeners. Panics if neither http nor httptls is set.
func (a *App) listens() {
	slog.Info("init listens")
	var err error
	if a.options.http == nil && a.options.httptls == nil {
		panic("need listen port")
	}
	if a.options.http != nil && a.options.httptls != nil {
		if a.options.http.Port == a.options.httptls.Port {
			// Same port: HTTP and TLS are demuxed on a single listener.
			slog.Info("both tcp tls")
			a.bothTCPTLS, err = net.Listen("tcp", a.options.http.String())
			if err != nil {
				panic(err)
			}
		} else {
			a.listenTCP()
			a.listenTLS()
		}
		return
	}
	if a.options.http != nil {
		a.listenTCP()
	}
	if a.options.httptls != nil {
		a.listenTLS()
	}
}

// startServe multiplexes HTTP1 and gRPC on a single listener using cmux.
func (a *App) startServe(l net.Listener) {
	slog.Info("server ", "listen", l.Addr())
	m := cmux.New(l)
	httpL := m.Match(cmux.HTTP1Fast())
	grpcL := m.MatchWithWriters(
		cmux.HTTP2MatchHeaderFieldPrefixSendSettings("content-type", "application/grpc"),
	)
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		if err := a.rpc.Serve(grpcL); err != nil {
			slog.Error("grpc stop", "error", err)
		}
	}()
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		if err := a.h1.Serve(httpL); err != nil {
			slog.Error("h1 stop", "error", err)
		}
	}()
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		if err := m.Serve(); err != nil {
			slog.Error("cmux stop", "error", err)
		}
	}()
	a.mList = append(a.mList, m)
}

// start launches all configured listeners and their goroutines.
func (a *App) start() {
	slog.Info("listen mux")
	if a.bothTCPTLS != nil {
		// Single port serving both plain HTTP/gRPC and TLS HTTP/gRPC.
		m := cmux.New(a.bothTCPTLS)
		grpcL := m.MatchWithWriters(
			cmux.HTTP2MatchHeaderFieldPrefixSendSettings("content-type", "application/grpc"),
		)
		httpL := m.Match(cmux.HTTP1Fast())
		other := m.Match(cmux.Any())

		cert, err := tls.LoadX509KeyPair(a.options.httptls.CertPath, a.options.httptls.KeyPath)
		if err != nil {
			panic(err)
		}
		cfg := &tls.Config{Certificates: []tls.Certificate{cert}}
		tlsL := tls.NewListener(other, cfg)
		tlsm := cmux.New(tlsL)
		grpcsL := tlsm.MatchWithWriters(
			cmux.HTTP2MatchHeaderFieldPrefixSendSettings("content-type", "application/grpc"),
		)
		httpsL := tlsm.Match(cmux.HTTP1Fast())

		a.wg.Add(1)
		go func() { defer a.wg.Done(); a.h1.Serve(httpL) }()
		a.wg.Add(1)
		go func() { defer a.wg.Done(); a.rpc.Serve(grpcL) }()
		a.wg.Add(1)
		go func() { defer a.wg.Done(); a.h1.Serve(httpsL) }()
		a.wg.Add(1)
		go func() { defer a.wg.Done(); a.rpc.Serve(grpcsL) }()
		a.wg.Add(1)
		go func() { defer a.wg.Done(); tlsm.Serve() }()
		a.wg.Add(1)
		go func() { defer a.wg.Done(); m.Serve() }()

		a.mList = append(a.mList, tlsm, m)
		return
	}
	if a.onlyTCP != nil {
		a.startServe(a.onlyTCP)
	}
	if a.onlyTLS != nil {
		a.startServe(a.onlyTLS)
	}
}

// runProm starts the Prometheus metrics server if configured.
func (a *App) runProm() {
	if a.options.prom != nil {
		met := http.NewServeMux()
		met.Handle("GET /metrics", promhttp.Handler())
		a.prom = &http.Server{Addr: a.options.prom.String(), Handler: met}
		a.wg.Add(1)
		go func() {
			defer a.wg.Done()
			if err := a.prom.ListenAndServe(); err != nil {
				slog.Error("run metrics server error", "error", err)
			}
		}()
	}
}

// configDefaultSlog replaces the default slog handler if WithSlogConfig was used.
func (a *App) configDefaultSlog() {
	if a.options.slog != nil {
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
}

// Run starts all servers and blocks until an OS signal is received,
// then performs a graceful shutdown.
func (a *App) Run() {
	a.configDefaultSlog()
	slog.Info("start server")
	a.listens()
	a.loadH1Handler()
	a.start()
	a.runProm()

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGHUP, syscall.SIGQUIT, syscall.SIGTERM, syscall.SIGINT)
	<-ch

	if a.h1 != nil {
		a.h1.Shutdown(context.Background())
	}
	if a.rpc != nil {
		a.rpc.GracefulStop()
	}
	if a.prom != nil {
		a.prom.Shutdown(context.Background())
	}
	for _, v := range a.mList {
		v.Close()
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
	slog.Info("initHTTPMux")
	var h http.Handler
	h = a.Router

	if a.options.enableGRPCWeb {
		wrappedGrpc := grpcweb.WrapServer(a.rpc)
		h = http.HandlerFunc(func(resp http.ResponseWriter, req *http.Request) {
			if wrappedGrpc.IsGrpcWebRequest(req) {
				wrappedGrpc.ServeHTTP(resp, req)
				return
			}
			a.Router.ServeHTTP(resp, req)
		})
	}
	if a.options.corsOptions != nil {
		h = cors.New(*a.options.corsOptions).Handler(h)
	}
	// Wrap with recovery, structured logging, and Prometheus metrics.
	mm := RecoveryMiddle(LogMidddle(MetricMiddle("app").Hander(h.ServeHTTP)))
	a.h1.Handler = mm
}

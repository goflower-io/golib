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

	h1 "github.com/goflower-io/golib/net/http"
)

type Addr struct {
	IP   string
	Port int
}

func (a *Addr) String() string {
	return fmt.Sprintf("%s:%d", a.IP, a.Port)
}

type TLSConfig struct {
	Addr
	KeyPath  string
	CertPath string
}

type SLogConfig struct {
	Level      slog.Level
	JSONOutPut bool
	AddSource  bool
}

type AppOptions struct {
	// if nil not run tls
	httptls *TLSConfig
	// if nil not run tcp // if Port == TLS.Port port run on same port
	http *Addr
	//
	enableGRPCWeb bool

	corsOptions *cors.Options

	prom *Addr

	slog *SLogConfig
}

type AppOption func(aos *AppOptions)

func WithTLSConfig(c *TLSConfig) AppOption {
	return func(aos *AppOptions) {
		aos.httptls = c
	}
}

func WithAddr(ip string, p int) AppOption {
	return func(aos *AppOptions) {
		aos.http = &Addr{IP: ip, Port: p}
	}
}

func WithPromAddr(ip string, p int) AppOption {
	return func(aos *AppOptions) {
		aos.prom = &Addr{IP: ip, Port: p}
	}
}

func WihtGrpcWeb(open bool) AppOption {
	return func(aos *AppOptions) {
		aos.enableGRPCWeb = open
	}
}

func WithCorsOptions(opt *cors.Options) AppOption {
	return func(aos *AppOptions) {
		aos.corsOptions = opt
	}
}

func WithSlogConfig(l *SLogConfig) AppOption {
	return func(aos *AppOptions) {
		aos.slog = l
	}
}

type App struct {
	options    *AppOptions
	bothTCPTLS net.Listener
	onlyTCP    net.Listener
	onlyTLS    net.Listener
	rpc        *grpc.Server
	h1         *http.Server
	wg         sync.WaitGroup
	mList      []cmux.CMux
	httpmux    *http.ServeMux
	prom       *http.Server
}

func New(options ...AppOption) *App {
	a := &App{
		options: &AppOptions{},
		rpc:     grpc.NewServer(),
		h1:      &http.Server{},
		wg:      sync.WaitGroup{},
		httpmux: http.NewServeMux(),
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
	a.onlyTLS, err = tls.Listen(
		"tcp",
		a.options.httptls.String(),
		cfg,
	)
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

func (a *App) listens() {
	slog.Info("init listens")
	var err error
	if a.options.http == nil && a.options.httptls == nil {
		panic("need listen port")
	}
	if a.options.http != nil && a.options.httptls != nil {
		if a.options.http.Port == a.options.httptls.Port {
			slog.Info("both tcp tls")
			a.bothTCPTLS, err = net.Listen(
				"tcp",
				a.options.http.String(),
			)
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

func (a *App) start() {
	slog.Info("listen mux")
	if a.bothTCPTLS != nil {
		m := cmux.New(a.bothTCPTLS)
		grpcL := m.MatchWithWriters(
			cmux.HTTP2MatchHeaderFieldPrefixSendSettings("content-type", "application/grpc"),
		)
		httpL := m.Match(cmux.HTTP1Fast())

		other := m.Match(cmux.Any())
		// load tls file
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
		go func() {
			defer a.wg.Done()
			a.h1.Serve(httpL)
		}()
		a.wg.Add(1)

		go func() {
			defer a.wg.Done()
			a.rpc.Serve(grpcL)
		}()
		a.wg.Add(1)

		go func() {
			defer a.wg.Done()
			a.h1.Serve(httpsL)
		}()
		a.wg.Add(1)

		go func() {
			defer a.wg.Done()
			a.rpc.Serve(grpcsL)
		}()
		a.wg.Add(1)

		go func() {
			defer a.wg.Done()
			tlsm.Serve()
		}()
		a.wg.Add(1)

		go func() {
			defer a.wg.Done()
			m.Serve()
		}()

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

func (a *App) Run() {
	a.configDefaultSlog()
	slog.Info("start server")
	a.listens()
	a.loadH1Handler()
	a.start()
	a.runProm()
	// wait sign to exit
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

func (a *App) RegisteGrpcService(desc *grpc.ServiceDesc, s any) {
	a.rpc.RegisterService(desc, s)
}

func (a *App) loadH1Handler() {
	slog.Info("initHTTPMux")
	var h http.Handler
	h = a.httpmux

	if a.options.enableGRPCWeb {
		wrappedGrpc := grpcweb.WrapServer(a.rpc)
		h = http.HandlerFunc(func(resp http.ResponseWriter, req *http.Request) {
			if wrappedGrpc.IsGrpcWebRequest(req) {
				wrappedGrpc.ServeHTTP(resp, req)
				return
			}
			// Fall back to other servers.
			a.httpmux.ServeHTTP(resp, req)
		})

	}
	if a.options.corsOptions != nil {
		h = cors.New(*a.options.corsOptions).Handler(h)
	}
	// recovery log metric hander
	mm := h1.RecoveryMiddle(h1.LogMidddle(h1.MetricMiddle("app").Hander(h.ServeHTTP)))

	a.h1.Handler = mm
}

func (a *App) GET(path string, h http.HandlerFunc) {
	a.httpmux.HandleFunc("GET "+path, h)
}

func (a *App) POST(path string, h http.HandlerFunc) {
	a.httpmux.HandleFunc("POST "+path, h)
}

func (a *App) PUT(path string, h http.HandlerFunc) {
	a.httpmux.HandleFunc("PUT "+path, h)
}

func (a *App) DELETE(path string, h http.HandlerFunc) {
	a.httpmux.HandleFunc("DELETE "+path, h)
}

func (a *App) HEAD(path string, h http.HandlerFunc) {
	a.httpmux.HandleFunc("HEAD "+path, h)
}

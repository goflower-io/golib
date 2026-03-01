package app

// Integration tests for the App serve() / cmux traffic routing layer.
//
// These tests verify that plain HTTP, plain gRPC, TLS HTTP, and TLS gRPC
// connections are each routed to the correct server — both when running on
// separate ports and when sharing a single port with isTLS demuxing.

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/soheilhy/cmux"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health/grpc_health_v1"
)

// -------------------------------------------------------
// Test helpers
// -------------------------------------------------------

// selfSignedCert generates a self-signed ECDSA certificate for 127.0.0.1,
// valid for one hour. Used to create TLS listeners in tests.
func selfSignedCert(t *testing.T) tls.Certificate {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatal(err)
	}
	privDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := tls.X509KeyPair(
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: privDER}),
	)
	if err != nil {
		t.Fatal(err)
	}
	return cert
}

// skipVerifyTLS returns a tls.Config that accepts any server certificate.
func skipVerifyTLS() *tls.Config { return &tls.Config{InsecureSkipVerify: true} }

// startApp registers a GET /ping route, wires up the HTTP handler chain,
// calls serve(l), and registers cleanup that shuts all goroutines down and
// waits for them to exit.
func startApp(t *testing.T, l net.Listener) *App {
	t.Helper()
	a := New()
	a.GET("/ping", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("pong"))
	})
	a.loadH1Handler()
	a.serve(l)
	t.Cleanup(func() {
		for _, m := range a.mList {
			m.Close()
		}
		a.h1.Close()
		a.rpc.Stop()
		a.wg.Wait()
	})
	return a
}

// waitForPort retries a TCP dial until the addr is reachable or 2 s elapse.
// It ensures the listener (and cmux) is ready before tests send requests.
func waitForPort(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		c, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			c.Close()
			time.Sleep(10 * time.Millisecond) // let cmux goroutine start
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("server not ready at %s within 2 s", addr)
}

// grpcHealthCheck calls the gRPC health service on conn and returns the status.
// The call times out after 5 s.
func grpcHealthCheck(t *testing.T, conn *grpc.ClientConn) grpc_health_v1.HealthCheckResponse_ServingStatus {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := grpc_health_v1.NewHealthClient(conn).Check(
		ctx, &grpc_health_v1.HealthCheckRequest{},
	)
	if err != nil {
		t.Fatalf("grpc health check: %v", err)
	}
	return resp.Status
}

// dialGRPC opens a gRPC connection to addr using the given dial options,
// blocking until the connection is established (or the context expires).
func dialGRPC(t *testing.T, addr string, opts ...grpc.DialOption) *grpc.ClientConn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	opts = append(opts, grpc.WithBlock())
	conn, err := grpc.DialContext(ctx, addr, opts...)
	if err != nil {
		t.Fatalf("grpc.DialContext(%s): %v", addr, err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

// -------------------------------------------------------
// TestIsTLS_Matcher — unit tests for the isTLS cmux.Matcher
// -------------------------------------------------------

func TestIsTLS_Matcher(t *testing.T) {
	cases := []struct {
		name  string
		first byte
		want  bool
	}{
		// 0x16 is the TLS record type for Handshake (ClientHello).
		{"TLS ClientHello (0x16)", 0x16, true},
		// Plain HTTP starts with a method letter.
		{"plain HTTP 'G'", 'G', false},
		{"plain HTTP 'P'", 'P', false},
		{"zero byte", 0x00, false},
		// Other TLS record types are not ClientHello.
		{"TLS ChangeCipherSpec (0x14)", 0x14, false},
		{"TLS Alert (0x15)", 0x15, false},
		// HTTP/2 PRI preface — plain gRPC.
		{"HTTP2 PRI preface", 'P', false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := io.MultiReader(
				strings.NewReader(string([]byte{c.first})),
				strings.NewReader("rest"),
			)
			if got := isTLS(r); got != c.want {
				t.Errorf("isTLS(0x%02x) = %v, want %v", c.first, got, c.want)
			}
		})
	}
}

// isTLS must return false (not panic) when the reader is already at EOF.
func TestIsTLS_EOF(t *testing.T) {
	if isTLS(strings.NewReader("")) {
		t.Error("isTLS on empty reader should return false")
	}
}

// -------------------------------------------------------
// TestServe_PlainHTTP — HTTP/1.1 traffic → http.Server
// -------------------------------------------------------

func TestServe_PlainHTTP(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	startApp(t, l)
	waitForPort(t, addr)

	resp, err := http.Get("http://" + addr + "/ping")
	if err != nil {
		t.Fatalf("GET /ping: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

// Unknown paths must return 404 through the HTTP server.
func TestServe_PlainHTTP_404(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	startApp(t, l)
	waitForPort(t, addr)

	resp, err := http.Get("http://" + addr + "/no-such-path")
	if err != nil {
		t.Fatalf("GET /no-such-path: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

// -------------------------------------------------------
// TestServe_PlainGRPC — gRPC/HTTP2 traffic → grpc.Server
// -------------------------------------------------------

func TestServe_PlainGRPC(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	startApp(t, l)
	waitForPort(t, addr)

	conn := dialGRPC(t, addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if status := grpcHealthCheck(t, conn); status != grpc_health_v1.HealthCheckResponse_SERVING {
		t.Fatalf("expected SERVING, got %v", status)
	}
}

// -------------------------------------------------------
// TestServe_TLSHTTP — TLS HTTP/1.1 traffic → http.Server via TLS listener
// -------------------------------------------------------

func TestServe_TLSHTTP(t *testing.T) {
	cert := selfSignedCert(t)
	raw, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := raw.Addr().String()
	startApp(t, tls.NewListener(raw, &tls.Config{Certificates: []tls.Certificate{cert}}))
	waitForPort(t, addr)

	client := &http.Client{Transport: &http.Transport{TLSClientConfig: skipVerifyTLS()}}
	resp, err := client.Get("https://" + addr + "/ping")
	if err != nil {
		t.Fatalf("HTTPS GET /ping: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

// -------------------------------------------------------
// TestServe_TLSgRPC — gRPC over TLS → grpc.Server via TLS listener
// -------------------------------------------------------

func TestServe_TLSgRPC(t *testing.T) {
	cert := selfSignedCert(t)
	raw, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := raw.Addr().String()
	startApp(t, tls.NewListener(raw, &tls.Config{Certificates: []tls.Certificate{cert}}))
	waitForPort(t, addr)

	conn := dialGRPC(t, addr, grpc.WithTransportCredentials(credentials.NewTLS(skipVerifyTLS())))
	if status := grpcHealthCheck(t, conn); status != grpc_health_v1.HealthCheckResponse_SERVING {
		t.Fatalf("expected SERVING, got %v", status)
	}
}

// -------------------------------------------------------
// TestSamePortDemux — isTLS outer cmux splits TLS/plain on one TCP port
// -------------------------------------------------------

// TestSamePortDemux verifies the same-port scenario from App.start():
//   - plain HTTP → plain serve() pipeline → http.Server
//   - plain gRPC → plain serve() pipeline → grpc.Server
//   - TLS  HTTP → TLS  serve() pipeline → http.Server
//   - TLS  gRPC → TLS  serve() pipeline → grpc.Server
func TestSamePortDemux(t *testing.T) {
	cert := selfSignedCert(t)
	tlsCfg := &tls.Config{Certificates: []tls.Certificate{cert}}

	raw, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := raw.Addr().String()

	a := New()
	a.GET("/ping", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("pong"))
	})
	a.loadH1Handler()

	// Mirror the same-port branch of App.start().
	outerMux := cmux.New(raw)
	tlsRaw := outerMux.Match(isTLS)
	plainL := outerMux.Match(cmux.Any())
	a.go_(func() { outerMux.Serve() })
	a.mList = append(a.mList, outerMux)

	a.serve(plainL)
	a.serve(tls.NewListener(tlsRaw, tlsCfg))

	t.Cleanup(func() {
		for _, m := range a.mList {
			m.Close()
		}
		a.h1.Close()
		a.rpc.Stop()
		a.wg.Wait()
	})

	waitForPort(t, addr)

	t.Run("plain_HTTP", func(t *testing.T) {
		resp, err := http.Get("http://" + addr + "/ping")
		if err != nil {
			t.Fatalf("plain HTTP GET: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("plain_gRPC", func(t *testing.T) {
		conn := dialGRPC(t, addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if status := grpcHealthCheck(t, conn); status != grpc_health_v1.HealthCheckResponse_SERVING {
			t.Fatalf("expected SERVING, got %v", status)
		}
	})

	t.Run("TLS_HTTP", func(t *testing.T) {
		client := &http.Client{Transport: &http.Transport{TLSClientConfig: skipVerifyTLS()}}
		resp, err := client.Get("https://" + addr + "/ping")
		if err != nil {
			t.Fatalf("TLS HTTP GET: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("TLS_gRPC", func(t *testing.T) {
		conn := dialGRPC(t, addr,
			grpc.WithTransportCredentials(credentials.NewTLS(skipVerifyTLS())),
		)
		if status := grpcHealthCheck(t, conn); status != grpc_health_v1.HealthCheckResponse_SERVING {
			t.Fatalf("expected SERVING, got %v", status)
		}
	})
}

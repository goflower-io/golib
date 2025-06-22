package main

import (
	"fmt"
	"net/http"

	"github.com/rs/cors"

	"github.com/goflower-io/golib/net/app"
)

func main() {
	a := app.New(
		app.WithAddr("0.0.0.0", 7788),
		app.WithTLSConfig(&app.TLSConfig{
			Addr: app.Addr{
				Port: 7890,
				IP:   "0.0.0.0",
			},
			KeyPath:  "./key.pem",
			CertPath: "./cert.pem",
		}),
		app.WithCorsOptions(&cors.Options{
			AllowedOrigins: []string{"*"},
			AllowedHeaders: []string{"*"},
			AllowedMethods: []string{"*"},
		}),
		app.WithPromAddr("127.0.0.1", 7789),
		app.WihtGrpcWeb(true),
	)
	a.GET("/hello", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "hello")
	}))
	a.GET("/greet", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "greet")
	}))

	a.Run()
}

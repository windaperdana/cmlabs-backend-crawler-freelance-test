package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/cmlabs/crawler/internal/api"
)

func main() {
	addr := flag.String("addr", envOr("CRAWLER_ADDR", ":9000"), "HTTP listen address")
	flag.Parse()

	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("[crawler] ")

	mux := api.Handler()

	srv := &http.Server{
		Addr:         *addr,
		Handler:      mux,
		ReadTimeout:  5 * time.Minute,
		WriteTimeout: 10 * time.Minute,
		IdleTimeout:  60 * time.Second,
	}

	log.Printf("starting server on %s", *addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
		os.Exit(1)
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

package main

import (
	"embed"
	"flag"
	"log"
	"net/http"
	"os"

	"hshow/internal/hledger"
	"hshow/internal/web"
)

//go:embed templates
var templatesFS embed.FS

func main() {
	addr := flag.String("addr", envOr("HSHOW_ADDR", "127.0.0.1:8080"), "address to listen on")
	journal := flag.String("journal", os.Getenv("HSHOW_JOURNAL"), "path to the hledger journal file (required)")
	hledgerBin := flag.String("hledger-bin", envOr("HSHOW_HLEDGER_BIN", "hledger"), "path to the hledger executable")
	flag.Parse()

	if *journal == "" {
		log.Fatal("missing required journal path: set -journal or HSHOW_JOURNAL")
	}

	runner := hledger.NewRunner(*hledgerBin, *journal)

	srv, err := web.NewServer(templatesFS, runner)
	if err != nil {
		log.Fatalf("initializing server: %v", err)
	}

	mux := http.NewServeMux()
	srv.Routes(mux)

	log.Printf("hshow listening on %s (journal=%s, hledger=%s)", *addr, *journal, *hledgerBin)
	if err := http.ListenAndServe(*addr, mux); err != nil {
		log.Fatal(err)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

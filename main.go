package main

import (
	"fmt"
	"net/http"
	"os"
	"sync/atomic"
)

type apiConfig struct {
	fileserverHits atomic.Int32
}

func main() {
	mux := http.NewServeMux()
	fileServer := http.FileServer(http.Dir("."))
	var apiCfg apiConfig
	mux.Handle("/app/", http.StripPrefix("/app/", apiCfg.middlewareMetricsInc(fileServer)))
	mux.HandleFunc("GET /metrics", apiCfg.handlerPrint)
	mux.HandleFunc("POST /reset", apiCfg.handlerReset)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("200 OK"))
	})
	mux.Handle("/app/assets", http.StripPrefix("/app/", http.FileServer(http.Dir("."))))
	server := &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}
	err := server.ListenAndServe()
	if err != nil {
		fmt.Print("Error al iniciar el servidor")
		os.Exit(1)
	}
}
func (cfg *apiConfig) middlewareMetricsInc(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg.fileserverHits.Add(1)
		next.ServeHTTP(w, r)
	})
}
func (cfg *apiConfig) handlerPrint(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprintf(w, "Hits: %d\n", cfg.fileserverHits.Load())
}

func (cfg *apiConfig) handlerReset(w http.ResponseWriter, r *http.Request) {
	cfg.fileserverHits.Store(0)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Counter reset.\n"))
}

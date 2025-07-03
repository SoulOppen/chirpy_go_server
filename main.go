package main

import (
	"chirpy/internal/database"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync/atomic"

	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
)

type apiConfig struct {
	fileserverHits atomic.Int32
	db             *database.Queries
}
type parameters struct {
	Body string `json:"body"`
}

func main() {
	godotenv.Load()
	dbURL := os.Getenv("DB_URL")
	db, err := sql.Open("postgres", dbURL)
	dbQueries := database.New(db)

	mux := http.NewServeMux()
	fileServer := http.FileServer(http.Dir("."))
	var apiCfg apiConfig
	apiCfg.db = dbQueries
	mux.Handle("/app/", http.StripPrefix("/app/", apiCfg.middlewareMetricsInc(fileServer)))
	mux.HandleFunc("GET /admin/metrics", apiCfg.handlerPrint)
	mux.HandleFunc("POST /admin/reset", apiCfg.handlerReset)
	mux.HandleFunc("POST /api/validate_chirp", apiCfg.handlerValid)
	mux.HandleFunc("GET /api/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("200 OK"))
	})
	mux.Handle("/app/assets", http.StripPrefix("/app/", http.FileServer(http.Dir("."))))
	server := &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}
	err = server.ListenAndServe()
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
	w.Header().Set("Content-Type", "text/html")
	count := cfg.fileserverHits.Load()
	html := fmt.Sprintf(`
<html>
  <body>
    <h1>Welcome, Chirpy Admin</h1>
    <p>Chirpy has been visited %d times!</p>
  </body>
</html>`, count)
	w.Write([]byte(html))
}

func (cfg *apiConfig) handlerReset(w http.ResponseWriter, r *http.Request) {
	cfg.fileserverHits.Store(0)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Counter reset.\n"))
}
func (cfg *apiConfig) handlerValid(w http.ResponseWriter, r *http.Request) {
	decoder := json.NewDecoder(r.Body)
	body := parameters{}
	err := decoder.Decode(&body)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(500)
		json := "{\"error\": \"Something went wrong\"}"
		w.Write([]byte(json))
	}
	if len(body.Body) > 140 {
		respondWithError(w, 400, "{\"error\": \"Chirp is too long\"}")
	} else {
		respondWithJSON(w, 200, body)
	}
}
func respondWithError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json := fmt.Sprintf(`{"error": "%s"}`, msg)
	w.Write([]byte(json))
}
func respondWithJSON(w http.ResponseWriter, code int, payload parameters) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	msg := payload.Body
	badWords := []string{"kerfuffle", "sharbert", "fornax"}
	words := strings.Fields(msg)

	for i, w := range words {
		lw := strings.ToLower(w) // palabra en minúsculas para comparar

		for _, b := range badWords {
			if lw == b {
				// Sustituye solo esa posición, conserva la longitud original
				words[i] = strings.Repeat("*", 4)
				break
			}
		}
	}
	json := fmt.Sprintf(`{"cleaned_body": "%s"}`, strings.Join(words, " "))
	w.Write([]byte(json))
}

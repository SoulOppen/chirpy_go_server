package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/SoulOppen/chirpy_go_server/internal/auth"
	"github.com/SoulOppen/chirpy_go_server/internal/database"
	"github.com/google/uuid"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
)

type apiConfig struct {
	fileserverHits atomic.Int32
	db             *database.Queries
}

type parameters struct {
	Body   string    `json:"body"`
	UserId uuid.UUID `json:"user_id"`
}

type mail struct {
	Password string `json:"password`
	Email    string `json:"email"`
}

type User struct {
	ID        uuid.UUID `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Email     string    `json:"email"`
}

type Chirp struct {
	ID        uuid.UUID `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Body      string    `json:"body"`
	UserId    uuid.UUID `json:"user_id"`
}

func main() {
	godotenv.Load()

	dbURL := os.Getenv("DB_URL")
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		fmt.Printf("%s\n", err)
		os.Exit(1)
	}
	defer db.Close()

	dbQueries := database.New(db)

	var apiCfg apiConfig
	apiCfg.db = dbQueries

	mux := http.NewServeMux()
	fileServer := http.FileServer(http.Dir("."))

	mux.Handle("/app/", http.StripPrefix("/app/", apiCfg.middlewareMetricsInc(fileServer)))
	mux.Handle("/app/assets", http.StripPrefix("/app/", http.FileServer(http.Dir("."))))
	mux.HandleFunc("GET /admin/metrics", apiCfg.handlerPrint)
	mux.HandleFunc("GET /api/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("200 OK"))
	})
	mux.HandleFunc("GET /api/chirps", apiCfg.handleGetChirps)
	mux.HandleFunc("GET /api/chirps/{chirpID}", apiCfg.handleGetOneChirp)
	mux.HandleFunc("POST /api/users", apiCfg.newUser)
	mux.HandleFunc("POST /admin/reset", apiCfg.handlerReset)
	mux.HandleFunc("POST /api/chirps", apiCfg.handlerValid)
	mux.HandleFunc("POST /api/login", apiCfg.handlerLogin)
	server := &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}
	err = server.ListenAndServe()
	if err != nil {
		fmt.Println("Error al iniciar el servidor")
		os.Exit(1)
	}
}

// GET /api/chirps
func (cfg *apiConfig) handleGetChirps(w http.ResponseWriter, r *http.Request) {
	dbChirps, err := cfg.db.GetChirps(context.Background())
	if err != nil {
		respondWithError(w, 500, "Fail to connect to DB")
		return
	}
	// Mapear a nuestro struct con tags JSON correctos
	chirps := make([]Chirp, len(dbChirps))
	for i, c := range dbChirps {
		chirps[i] = Chirp{
			ID:        c.ID,
			CreatedAt: c.CreatedAt,
			UpdatedAt: c.UpdatedAt,
			Body:      c.Body,
			UserId:    c.UserID,
		}
	}

	respondWithJSON(w, 200, chirps)
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
	err := cfg.db.Reset(r.Context())
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Failed to reset the database: " + err.Error()))
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Counter reset.\n"))
}

// POST /api/chirps
func (cfg *apiConfig) handlerValid(w http.ResponseWriter, r *http.Request) {
	decoder := json.NewDecoder(r.Body)
	body := parameters{}
	err := decoder.Decode(&body)
	if err != nil {
		respondWithError(w, 500, "Invalid JSON body")
		return
	}

	if len(body.Body) > 140 {
		respondWithError(w, 400, "Chirp is too long")
		return
	}

	msg := body.Body
	badWords := []string{"kerfuffle", "sharbert", "fornax"}
	words := strings.Fields(msg)

	for i, w := range words {
		lw := strings.ToLower(w)
		for _, b := range badWords {
			if lw == b {
				words[i] = strings.Repeat("*", 4)
				break
			}
		}
	}

	newChirp := database.InsertChirpsParams{
		Body:   strings.Join(words, " "),
		UserID: body.UserId,
	}

	chirp, err := cfg.db.InsertChirps(context.Background(), newChirp)
	if err != nil {
		fmt.Printf("Error inserting chirp: %v\n", err)
		respondWithError(w, 400, "Could not insert chirp")
		return
	}

	respondWithJSON(w, 201, Chirp{
		ID:        chirp.ID,
		CreatedAt: chirp.CreatedAt,
		UpdatedAt: chirp.UpdatedAt,
		Body:      chirp.Body,
		UserId:    chirp.UserID,
	})
}

// POST /api/users
func (cfg *apiConfig) newUser(w http.ResponseWriter, r *http.Request) {
	var inputMail mail
	decoder := json.NewDecoder(r.Body)
	err := decoder.Decode(&inputMail)
	if err != nil {
		respondWithError(w, 500, "Invalid JSON body")
		return
	}
	hashPass, err := auth.HashPassword(inputMail.Password)
	if err != nil {
		fmt.Println(err)
	}
	user, err := cfg.db.CreateUser(r.Context(), database.CreateUserParams{Email: inputMail.Email, HashedPassword: hashPass})
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't create user")
		return
	}
	userNew := User{
		ID:        user.ID,
		CreatedAt: user.CreatedAt,
		UpdatedAt: user.UpdatedAt,
		Email:     user.Email,
	}

	respondWithJSON(w, 201, userNew)
}
func (cfg *apiConfig) handleGetOneChirp(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("chirpID")
	chirpID, err := uuid.Parse(id)
	fmt.Println(id)
	if err != nil {
		respondWithError(w, 404, "No tiene el formato correcto")
		return
	}
	chirp, err := cfg.db.OneChirps(context.Background(), chirpID)
	if err != nil {
		respondWithError(w, 404, "Bad id")
		return
	}
	respondWithJSON(w, 200, Chirp{
		ID:        chirp.ID,
		CreatedAt: chirp.CreatedAt,
		UpdatedAt: chirp.UpdatedAt,
		UserId:    chirp.UserID,
		Body:      chirp.Body,
	})
}
func (cfg *apiConfig) handlerLogin(w http.ResponseWriter, r *http.Request) {
	var inputMail mail
	decoder := json.NewDecoder(r.Body)
	err := decoder.Decode(&inputMail)
	if err != nil {
		w.WriteHeader(401)
	}
	CheckPassword, err := cfg.db.ReturnHashPassword(context.Background(), inputMail.Email)
	if err != nil {
		w.WriteHeader(401)
	}
	err = auth.CheckPasswordHash(inputMail.Password, CheckPassword)
	if err != nil {
		w.WriteHeader(401)
	}
	noPass, err := cfg.db.ReturnUserNotPassword(context.Background(), inputMail.Email)
	if err != nil {
		w.WriteHeader(401)
	}
	respondWithJSON(w, 200, User{
		ID:        noPass.ID,
		CreatedAt: noPass.CreatedAt,
		UpdatedAt: noPass.UpdatedAt,
		Email:     noPass.Email,
	})
}
func respondWithError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	payload := map[string]string{"error": msg}
	json.NewEncoder(w).Encode(payload)
}

func respondWithJSON(w http.ResponseWriter, code int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(payload)
}

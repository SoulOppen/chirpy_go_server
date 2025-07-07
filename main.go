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
	secret         string
	polkaKey       string
}

type parameters struct {
	Body   string    `json:"body"`
	UserId uuid.UUID `json:"user_id"`
}

type mail struct {
	Password string
	Email    string
}

type User struct {
	ID          uuid.UUID `json:"id"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	Email       string    `json:"email"`
	IsChirpyRed bool      `json:"is_chirpy_red"`
}
type param struct {
	User         User   `json:"user"`
	Token        string `json:"token"`
	RefreshToken string `json:"refresh_token"`
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
	secret := os.Getenv("SECRET_STRING")
	polkaKey := os.Getenv("POLKA_KEY")
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		fmt.Printf("%s\n", err)
		os.Exit(1)
	}
	defer db.Close()

	dbQueries := database.New(db)

	var apiCfg apiConfig
	apiCfg.db = dbQueries
	apiCfg.secret = secret
	apiCfg.polkaKey = polkaKey
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
	mux.HandleFunc("POST /api/refresh", apiCfg.handlerRefresh)
	mux.HandleFunc("POST /api/revoke", apiCfg.handlerRevoke)
	mux.HandleFunc("POST /api/polka/webhooks", apiCfg.handlerHook)
	mux.HandleFunc("PUT /api/users", apiCfg.handlerUpdate)
	mux.HandleFunc("DELETE /api/chirps/{chirpID}", apiCfg.handlerDelete)
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
	tokenString, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, 401, "Error")
		return
	}

	userID, err := auth.ValidateJWT(tokenString, cfg.secret)
	if err != nil {
		respondWithError(w, 401, "error")
		return
	}
	decoder := json.NewDecoder(r.Body)
	body := parameters{}
	err = decoder.Decode(&body)
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
		UserID: userID,
	}

	chirp, err := cfg.db.InsertChirps(context.Background(), newChirp)
	if err != nil {
		fmt.Printf("Error inserting chirp: %v\n", err)
		respondWithError(w, 401, "Could not insert chirp")
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
		ID:          user.ID,
		CreatedAt:   user.CreatedAt,
		UpdatedAt:   user.UpdatedAt,
		Email:       user.Email,
		IsChirpyRed: user.IsChirpyRed.Bool,
	}

	respondWithJSON(w, 201, userNew)
}
func (cfg *apiConfig) handleGetOneChirp(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("chirpID")
	chirpID, err := uuid.Parse(id)
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
		respondWithError(w, 401, "error")
	}
	CheckPassword, err := cfg.db.ReturnHashPassword(context.Background(), inputMail.Email)
	if err != nil {
		respondWithError(w, 401, "erorr")
	}
	err = auth.CheckPasswordHash(inputMail.Password, CheckPassword)
	if err != nil {
		respondWithError(w, 401, "error")
	}
	noPass, err := cfg.db.ReturnUserNotPassword(context.Background(), inputMail.Email)
	if err != nil {
		respondWithError(w, 401, "error")
	}
	tokenStr, err := auth.MakeJWT(noPass.ID, cfg.secret, 3600)
	if err != nil {
		respondWithError(w, 401, "error")
	}
	refreshTokenStr, err := auth.MakeRefreshToken()
	if err != nil {
		respondWithError(w, 401, "error")
	}
	expiresAt := time.Now().Add(7 * 24 * time.Hour)

	rt, err := cfg.db.RefreshToken(context.Background(), database.RefreshTokenParams{
		Token:  refreshTokenStr,
		UserID: noPass.ID,
		ExpiresAt: sql.NullTime{
			Time:  expiresAt,
			Valid: true,
		},
		RevokedAt: sql.NullTime{
			Valid: false,
		},
	})
	if err != nil {
		respondWithError(w, 401, "error")
	}
	respondWithJSON(w, 200, struct {
		ID           uuid.UUID `json:"id"`
		CreatedAt    time.Time `json:"created_at"`
		UpdatedAt    time.Time `json:"updated_at"`
		Email        string    `json:"email"`
		IsChirpyRed  bool      `json:"is_chirpy_red"`
		Token        string    `json:"token"`
		RefreshToken string    `json:"refresh_token"`
	}{
		ID:           noPass.ID,
		CreatedAt:    noPass.CreatedAt,
		UpdatedAt:    noPass.UpdatedAt,
		Email:        noPass.Email,
		IsChirpyRed:  noPass.IsChirpyRed.Bool,
		Token:        tokenStr,
		RefreshToken: rt.Token,
	})
}
func (cfg *apiConfig) handlerRefresh(w http.ResponseWriter, r *http.Request) {
	type response struct {
		Token string `json:"token"`
	}
	tokenString, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, 401, "Error")
		return
	}
	idUser, err := cfg.db.GetUserFromRefreshToken(context.Background(), tokenString)
	if err != nil {
		respondWithError(w, 401, "Error")
		return
	}
	newToken, err := auth.MakeJWT(idUser, cfg.secret, 3600)
	if err != nil {
		respondWithError(w, 401, "Could not create token")
		return
	}
	respondWithJSON(w, http.StatusOK, response{
		Token: newToken,
	})
}
func (cfg *apiConfig) handlerRevoke(w http.ResponseWriter, r *http.Request) {
	tokenString, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, 401, "Error")
		return
	}
	_, err = cfg.db.UpdateRefreshToken(context.Background(), tokenString)
	if err != nil {
		respondWithError(w, 401, "Error")
		return
	}
	w.WriteHeader(204)
}
func (cfg *apiConfig) handlerUpdate(w http.ResponseWriter, r *http.Request) {
	tokenString, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, 401, "Error")
		return
	}
	userID, err := auth.ValidateJWT(tokenString, cfg.secret)
	if err != nil {
		respondWithError(w, 401, "Couldn't validate JWT")
		return
	}
	decoder := json.NewDecoder(r.Body)
	Email := mail{}
	err = decoder.Decode(&Email)
	if err != nil {
		respondWithError(w, 401, "Couldn't decode parameters")
		return
	}
	hashedPassword, err := auth.HashPassword(Email.Password)
	if err != nil {
		respondWithError(w, 401, "Couldn't hash password")
		return
	}
	user, err := cfg.db.UpdateUser(context.Background(), database.UpdateUserParams{
		ID:             userID,
		Email:          Email.Email,
		HashedPassword: hashedPassword,
	})
	if err != nil {
		respondWithError(w, 401, "Error al actualizar")
		return
	}
	respondWithJSON(w, 200, User{
		ID:          user.ID,
		CreatedAt:   user.CreatedAt,
		UpdatedAt:   user.UpdatedAt,
		Email:       user.Email,
		IsChirpyRed: user.IsChirpyRed.Bool,
	})
}
func (cfg *apiConfig) handlerDelete(w http.ResponseWriter, r *http.Request) {
	idChirp := r.PathValue("chirpID")

	tokenString, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, 401, "Not token")
		return
	}

	idUser, err := auth.ValidateJWT(tokenString, cfg.secret)
	if err != nil {
		respondWithError(w, 401, "Couldn't decode parameters")
		return
	}

	u, err := uuid.Parse(idChirp)
	if err != nil {
		respondWithError(w, 400, "id is not uuid")
		return
	}

	chirp, err := cfg.db.OneChirps(context.Background(), u)
	if err != nil {
		respondWithError(w, 404, "Chirp not found")
		return
	}

	if chirp.UserID != idUser {
		respondWithError(w, 403, "Fail to delete no authorize")
		return
	}

	_, err = cfg.db.DeleteChirp(context.Background(), u)
	if err != nil {
		respondWithError(w, 400, "Fail to delete")
		return
	}

	w.WriteHeader(204)
}
func (cfg *apiConfig) handlerHook(w http.ResponseWriter, r *http.Request) {

	type Param struct {
		Event string `json:"event"`
		Data  struct {
			UserID string `json:"user_id"`
		} `json:"data"`
	}
	apikey, err := auth.GetAPIKey(r.Header)
	if err != nil {
		respondWithError(w, 401, "doesn't exist apikey")
		return

	}
	if apikey != cfg.polkaKey {
		respondWithError(w, 401, "not authorize")
	}
	var param Param
	decoder := json.NewDecoder(r.Body)
	err = decoder.Decode(&param)
	if err != nil {
		respondWithError(w, 400, "no se pudo decodificar")
		return
	}
	if param.Event != "user.upgraded" {
		w.WriteHeader(204)
		return
	}
	u, err := uuid.Parse(param.Data.UserID)
	if err != nil {
		respondWithError(w, 400, "can't convert id")
		return
	}
	_, err = cfg.db.UpdateUserIsRed(context.Background(), u)
	if err != nil {
		respondWithError(w, 404, "not user id")
		return
	}
	w.WriteHeader(204)

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

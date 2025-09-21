package main

import (
	//stdlib
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync/atomic"

	//chirpy
	"chirpy/internal/database"

	//dependencies

	"github.com/gofrs/uuid/v5"
	_ "github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/joho/godotenv/autoload"
)

type apiConfig struct {
	fileserverHits atomic.Int32
	dbQueries      *database.Queries
}

func main() {
	const filepathRoot = "."
	const port = "8080"

	mux := http.NewServeMux()
	server := &http.Server{
		Handler: mux,
		Addr:    ":" + port,
	}

	dbURL := os.Getenv("DB_URL")
	if len(dbURL) == 0 {
		log.Fatalln("ENV_ERROR: DB_URL cannot be blank")
	}

	ctx := context.Background()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		log.Fatalf("DBPOOL_ERROR: %s\n", err)
	}
	defer pool.Close()

	dbQueries := database.New(pool)

	cfg := apiConfig{dbQueries: dbQueries}

	mux.Handle("/app/", cfg.middlewareMetricsInc(http.StripPrefix("/app", http.FileServer(http.Dir(filepathRoot)))))
	mux.HandleFunc("GET /api/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Content-Type", "text/plain; charset=utf-8")
		w.Write([]byte(http.StatusText(http.StatusOK)))
	})
	mux.HandleFunc("POST /api/users", func(w http.ResponseWriter, r *http.Request) {
		type parameters struct {
			Email string `json:"email"`
		}

		decoder := json.NewDecoder(r.Body)
		params := parameters{}
		err := decoder.Decode(&params)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			log.Fatalf("JSON_ERROR: Could not decode parameters\nERROR_BODY: %s\n", err)
		}

		user, err := cfg.dbQueries.CreateUser(r.Context(), params.Email)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			log.Fatalf("SQLC_ERROR: Could not create user\nERROR_BODY: %s\n", err)
		}

		type responseBody struct {
			ID        uuid.UUID        `json:"id"`
			CreatedAt pgtype.Timestamp `json:"created_at"`
			UpdatedAt pgtype.Timestamp `json:"updated_at"`
			Email     string           `json:"email"`
		}

		respBody := responseBody{
			ID:        user.ID,
			CreatedAt: user.CreatedAt,
			UpdatedAt: user.UpdatedAt,
			Email:     user.Email,
		}

		data, err := json.Marshal(respBody)
		if err != nil {
			log.Fatalf("JSON_ERROR: Could not encode response body.\nERROR_VAL: %s\n", err)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		w.Write(data)
		log.Printf("%s\n", data)
	})
	mux.HandleFunc("POST /api/chirps", func(w http.ResponseWriter, r *http.Request) {
		type parameters struct {
			Body   string    `json:"body"`
			UserID uuid.UUID `json:"user_id"`
		}

		decoder := json.NewDecoder(r.Body)
		params := parameters{}
		err := decoder.Decode(&params)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			log.Fatalf("JSON_ERROR: Could not decode parameters\nERROR_BODY: %s\n", err)
		}

		type responseBody struct {
			Valid     bool             `json:"valid"`
			Error     string           `json:"error"`
			ID        uuid.UUID        `json:"id"`
			CreatedAt pgtype.Timestamp `json:"created_at"`
			UpdatedAt pgtype.Timestamp `json:"updated_at"`
			Body      string           `json:"body"`
			UserID    uuid.UUID        `json:"user_id"`
		}

		chirp, err := cfg.dbQueries.CreateChirp(ctx, database.CreateChirpParams{
			Body:   params.Body,
			UserID: params.UserID,
		})
		if err != nil {
			log.Fatalf("DB_ERROR: Could not create chirp\nERROR_BODY: %s\n", err)
		}

		respBody := responseBody{
			Valid:     true,
			Error:     "",
			ID:        chirp.ID,
			CreatedAt: chirp.CreatedAt,
			UpdatedAt: chirp.UpdatedAt,
			UserID:    chirp.UserID,
		}

		status := http.StatusCreated

		if len(params.Body) > 140 {
			respBody.Valid = false
			respBody.Error = "This chirp is too long"
			status = http.StatusBadRequest
		}

		bannedWords := []string{
			"kerfuffle",
			"sharbert",
			"fornax",
		}

		bodyTokens := strings.Split(chirp.Body, " ")
		for index, token := range bodyTokens {
			for _, word := range bannedWords {
				if strings.EqualFold(token, word) {
					bodyTokens[index] = "****"
				}
			}
		}

		chirp.Body = strings.Join(bodyTokens, " ")
		respBody.Body = chirp.Body

		data, err := json.Marshal(respBody)
		if err != nil {
			log.Fatalf("JSON_ERROR: Could not encode response body\nERROR_BODY: %s\n", err)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		w.Write(data)
		log.Printf("%s\n", data)
	})
	mux.HandleFunc("GET /api/chirps", func(w http.ResponseWriter, r *http.Request) {
		chirps, err := cfg.dbQueries.GetAllChirps(ctx)
		if err != nil {
			log.Fatalf("DB_ERROR: Could not collect chirps\nERROR_BODY: %s\n", err)
		}

		data, err := json.Marshal(chirps)
		if err != nil {
			log.Fatalf("JSON_ERROR: Could not encode json\nERROR_BODY: %s\n", err)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(data)
		log.Printf("%s\n", data)
	})
	mux.HandleFunc("GET /api/chirps/{chirpID}", func(w http.ResponseWriter, r *http.Request) {
		chirpID := r.PathValue("chirpID")
		log.Printf("A GET request was made to /api/chirps/%s\n", chirpID)

		chirpUUID := uuid.FromStringOrNil(chirpID)

		chirp, err := cfg.dbQueries.GetChirp(ctx, chirpUUID)
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		data, err := json.Marshal(chirp)
		if err != nil {
			log.Fatalf("JSON_ERROR: Could not encode json\nERROR_BODY: %s\n", err)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(data)
	})
	mux.HandleFunc("GET /admin/metrics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Content-Type", "text/plain; charset=utf-8")
		w.Write(fmt.Appendf([]byte(""), `<html>
  <body>
    <h1>Welcome, Chirpy Admin</h1>
    <p>Chirpy has been visited %d times!</p>
  </body>
</html>`, cfg.fileserverHits.Load()))
	})
	mux.HandleFunc("POST /admin/reset", cfg.dataReset)

	log.Printf("Serving files from %s:%s\n", filepathRoot, port)
	log.Fatal(server.ListenAndServe())
}

func (cfg *apiConfig) middlewareMetricsInc(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg.fileserverHits.Add(1)
		next.ServeHTTP(w, r)
	})
}

func (cfg *apiConfig) dataReset(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Content-Type", "text/plain; charset=utf-8")
	cfg.fileserverHits.Store(0)
	platform := os.Getenv("PLATFORM")
	if len(platform) == 0 {
		log.Fatalf("ENV_ERROR: PLATFORM cannot be an empty string.")
	}
	if platform != "dev" {
		w.WriteHeader(http.StatusForbidden)
		log.Println("PLATFORM is not 'dev'. User database will not be reset.")
		return
	}
	cfg.dbQueries.DeleteUsers(r.Context())
}

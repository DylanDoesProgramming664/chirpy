package main

import (
	"chirpy/internal/database"
	"context"

	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync/atomic"

	pgx "github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/joho/godotenv"
)

type apiConfig struct {
	fileserverHits atomic.Int32
	dbQueries      *database.Queries
}

func main() {
	godotenv.Load()
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

	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		log.Fatalf("DB_ERROR: %s\n", err)
	}
	defer conn.Close(ctx)

	dbQueries := database.New(conn)

	cfg := apiConfig{dbQueries: dbQueries}

	mux.Handle("/app/", cfg.middlewareMetricsInc(http.StripPrefix("/app", http.FileServer(http.Dir(filepathRoot)))))
	mux.HandleFunc("GET /api/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Content-Type", "text/plain; charset=utf-8")
		w.Write([]byte(http.StatusText(http.StatusOK)))
	})
	mux.HandleFunc("POST /api/validate_chirp", func(w http.ResponseWriter, r *http.Request) {
		type parameters struct {
			Body string `json:"body"`
		}

		decoder := json.NewDecoder(r.Body)
		params := parameters{}
		err := decoder.Decode(&params)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			log.Fatalf("JSON_ERROR: Could not decode parameters\nERROR_MSG: %s\n", err)
		}

		type responseBody struct {
			Valid       bool   `json:"valid"`
			Error       string `json:"error"`
			CleanedBody string `json:"cleaned_body"`
		}

		respBody := responseBody{
			Valid:       true,
			Error:       "",
			CleanedBody: params.Body,
		}

		status := http.StatusOK

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

		bodyTokens := strings.Split(respBody.CleanedBody, " ")
		for index, token := range bodyTokens {
			for _, word := range bannedWords {
				if strings.EqualFold(token, word) {
					bodyTokens[index] = "****"
				}
			}
		}

		respBody.CleanedBody = strings.Join(bodyTokens, " ")

		data, err := json.Marshal(respBody)
		if err != nil {
			log.Fatalf("JSON_ERROR: Could not encode response body\nERROR_MSG: %s\n", err)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		w.Write(data)
		log.Printf("%s\n", data)
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
			log.Fatalf("JSON_ERROR: Could not decode parameters\nERROR_MSG: %s\n", err)
		}

		user, err := cfg.dbQueries.CreateUser(r.Context(), params.Email)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			log.Fatalf("SQLC_ERROR: Could not create user\nERROR_MSG: %s\n", err)
		}

		type responseBody struct {
			ID        pgtype.UUID      `json:"id"`
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

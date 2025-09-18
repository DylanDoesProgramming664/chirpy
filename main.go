package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync/atomic"

	_ "github.com/jackc/pgx/v5"
)

type apiConfig struct {
	fileserverHits atomic.Int32
}

func main() {
	const filepathRoot = "."
	const port = "8080"

	cfg := apiConfig{}

	mux := http.NewServeMux()
	server := &http.Server{
		Handler: mux,
		Addr:    ":" + port,
	}

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
			log.Printf("Error decoding parameters: %s\n", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		type returnVals struct {
			Valid       bool   `json:"valid"`
			Error       string `json:"error"`
			CleanedBody string `json:"cleaned_body"`
		}

		respBody := returnVals{
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
			log.Printf("Error marshalling JSON: %s\n", err)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		w.Write(append(data, '\n'))
		log.Printf("%+v\n", string(data))
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
	mux.HandleFunc("POST /admin/reset", cfg.metricsReset)

	log.Printf("Serving files from %s on port: %s", filepathRoot, port)
	log.Fatal(server.ListenAndServe())
}

func (cfg *apiConfig) middlewareMetricsInc(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg.fileserverHits.Add(1)
		next.ServeHTTP(w, r)
	})
}

func (cfg *apiConfig) metricsReset(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Content-Type", "text/plain; charset=utf-8")
	cfg.fileserverHits.Store(0)
}

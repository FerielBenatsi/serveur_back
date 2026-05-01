package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sportpulse/server/handlers"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

func enableCORS(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
}

func withCORS(handler func(http.ResponseWriter, *http.Request)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		enableCORS(w)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		handler(w, r)
	}
}

func handlerHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"message": "SportPulse server is running",
	})
}

func handlerNotFound(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	json.NewEncoder(w).Encode(map[string]string{
		"error": fmt.Sprintf("route '%s' not found", r.URL.Path),
	})
}

func main() {
	godotenv.Load()

	initDB() // initialise la base

	StartScheduler() // lancer le scheduler nocturne en arrière-plan
	go func() {
		time.Sleep(3 * time.Second)
		FetchAndStoreScheduled()
	}()

	// handlers.InsertTestMatches(DB) // désactivé en prod — le scheduler récupère les vrais matchs

	mux := http.NewServeMux() // configurer les routes HTTP

	// ------- Health -----------------------------------------
	mux.HandleFunc("/api/health", withCORS(handlerHealth))

	// -------- Auth ------------------------------------------
	mux.HandleFunc("/api/auth/register", withCORS(func(w http.ResponseWriter, r *http.Request) {
		handlers.Register(DB, w, r)
	}))
	mux.HandleFunc("/api/auth/login", withCORS(func(w http.ResponseWriter, r *http.Request) {
		handlers.Login(DB, w, r)
	}))

	// -------- Matchs ----------------------------------------
	mux.HandleFunc("/api/matches", withCORS(func(w http.ResponseWriter, r *http.Request) {
		handlers.GetMatches(DB, w, r)
	}))

	mux.HandleFunc("/api/matches/", withCORS(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(r.URL.Path, "/")
		// parts : ["", "api", "matches", "{id}", "{sub}"]

		if len(parts) == 4 {
			handlers.GetMatchByID(DB, w, r, CheckAndUpdateMatch)
		} else if len(parts) == 5 && parts[4] == "predictions" {
			if r.Method == http.MethodGet {
				handlers.GetPredictions(DB, w, r)
			} else {
				handlers.PostPrediction(DB, w, r)
			}
		} else if len(parts) == 5 && parts[4] == "comments" {
			if r.Method == http.MethodGet {
				handlers.GetComments(DB, w, r)
			} else {
				handlers.PostComment(DB, w, r)
			}
		} else {
			handlerNotFound(w, r)
		}
	}))

	// ----------- Leaderboard --------------------------------
	mux.HandleFunc("/api/leaderboard", withCORS(func(w http.ResponseWriter, r *http.Request) {
		handlers.GetLeaderboard(DB, w, r)
	}))

	// ----------- Users --------------------------------------
	mux.HandleFunc("/api/users/me", withCORS(func(w http.ResponseWriter, r *http.Request) {
		handlers.GetMe(DB, w, r)
	}))
	mux.HandleFunc("/api/users/", withCORS(func(w http.ResponseWriter, r *http.Request) {
		handlers.GetUserByID(DB, w, r)
	}))

	// ---------- Route inconnue ------------------------------
	mux.HandleFunc("/", handlerNotFound)

	// ---------- Démarrage serveur ---------------------------
	// Render fournit le port via la variable d'environnement PORT
	// En local, on utilise 8091 par défaut
	port := os.Getenv("PORT")
	if port == "" {
		port = "8091"
	}
	log.Printf("SportPulse server starting on port %s\n", port)

	err := http.ListenAndServe(":"+port, mux)
	if err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}
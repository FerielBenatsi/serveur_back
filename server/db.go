package main

import (
	"database/sql"
	"log"

	_ "github.com/mattn/go-sqlite3"
)

// DB est la variable globale de connexion a la base de donnees
// Elle est accessible depuis tous les fichiers du package main
var DB *sql.DB

// initDB ouvre le connexion SQLite et cree les tables si elles n'existent pas encore
func initDB() {
	var err error

	//Ouvre (ou cree) le fichier sport.db dans le dossier courant
	DB, err = sql.Open("sqlite3", "./sport.db")
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}

	//Vérifie que la connexion fonctionne vraiment
	if err = DB.Ping(); err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	//Cree les tables
	createTables()
	log.Println("Database connected and tables ready")

}

//createTables cree les 4 tables du projet si elles n'existent pas encore

func createTables() {
	//Table users - les comptes utilisateurs
	_, err := DB.Exec(`
	CREATE TABLE IF NOT EXISTS users(
	id                INTEGER PRIMARY KEY AUTOINCREMENT,
	username          TEXT    NOT NULL UNIQUE,
	email             TEXT    NOT NULL UNIQUE,
	password_hash     TEXT    NOT NULL,
	points_total      INTEGER NOT NULL DEFAULT 0,
	created_at        DATETIME DEFAULT CURRENT_TIMESTAMP
	
	)

	`)
	if err != nil {
		log.Fatalf("Failed to create table users: %v", err)
	}

	//Table matches - les matchs de football (cache local de l'API)

	_, err = DB.Exec(`
	 CREATE TABLE IF NOT EXISTS matches (
	 id           INTEGER PRIMARY KEY AUTOINCREMENT,
	 api_id       INTEGER NOT NULL UNIQUE,
	 home_team    TEXT NOT NULL,
	 away_team    TEXT NOT NULL,
	 match_date   DATETIME NOT NULL,
	 home_score   INTEGER,
	 away_score   INTEGER,
	 status       TEXT NOT NULL DEFAULT 'SCHEDULED',
	 competition  TEXT NOT NULL,
	 updated_at   DATETIME DEFAULT CURRENT_TIMESTAMP
	 )
	 
	 `)

	if err != nil {
		log.Fatalf("Failed to create table matches: %v", err)
	}

	//Table predictions - les prononstics soumis par les utilisateurs
	_, err = DB.Exec(`
	 CREATE TABLE IF NOT EXISTS predictions(
	 id             INTEGER PRIMARY KEY AUTOINCREMENT,
	 user_id        INTEGER NOT NULL,
	 match_id       INTEGER NOT NULL,
	 home_score_pred INTEGER NOT NULL,
	 away_score_pred INTEGER NOT NULL,
	 points_earned  INTEGER,
	 created_at     DATETIME DEFAULT CURRENT_TIMESTAMP,
	 FOREIGN KEY (user_id) REFERENCES users(id),
	 FOREIGN KEY (match_id) REFERENCES matches(id),
	 UNIQUE (user_id, match_id)
	 )
	 `)

	if err != nil {
		log.Fatalf("Failed to create predictions: %v", err)
	}

	//Table comments - les commentaires sous chaque match
	_, err = DB.Exec(`
	 CREATE TABLE IF NOT EXISTS comments(
	 id              INTEGER PRIMARY KEY AUTOINCREMENT,
	 user_id         INTEGER NOT NULL,
	 match_id        INTEGER NOT NULL,
	 content         TEXT NOT NULL,
	 created_at      DATETIME DEFAULT CURRENT_TIMESTAMP,
	 FOREIGN KEY     (user_id) REFERENCES users(id),
	 FOREIGN KEY     (match_id) REFERENCES matches(id)
	 )
	 
	 `)
	if err != nil {
		log.Fatalf("Failed to create table comments: %v", err)
	}

}

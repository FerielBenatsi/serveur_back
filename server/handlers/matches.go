package handlers

import (
	"database/sql"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Match struct {
	ID          int    `json:"id"`
	ApiID       int    `json:"api_id"`
	HomeTeam    string `json:"home_team"`
	AwayTeam    string `json:"away_team"`
	MatchDate   string `json:"match_date"`
	HomeScore   *int   `json:"home_score"`
	AwayScore   *int   `json:"away_score"`
	Status      string `json:"status"`
	Competition string `json:"competition"`
	UpdatedAt   string `json:"updated_at"`
}

func GetMatches(db *sql.DB, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	in7days := time.Now().UTC().Add(7 * 24 * time.Hour).Format("2006-01-02 15:04:05")
	rows, err := db.Query(`
		SELECT id, api_id, home_team, away_team, match_date,
		       home_score, away_score, status, competition, updated_at
		FROM matches
		WHERE match_date >= ? AND match_date <= ?
		ORDER BY match_date ASC`, now, in7days)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	defer rows.Close()

	matches := []Match{}
	for rows.Next() {
		var m Match
		err := rows.Scan(
			&m.ID, &m.ApiID, &m.HomeTeam, &m.AwayTeam, &m.MatchDate,
			&m.HomeScore, &m.AwayScore, &m.Status, &m.Competition, &m.UpdatedAt,
		)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "scan error")
			return
		}
		matches = append(matches, m)
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"matches": matches,
		"count":   len(matches),
	})
}

func GetMatchByID(db *sql.DB, w http.ResponseWriter, r *http.Request,
	checkFn func(db *sql.DB, apiID, internalID int)) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 4 || parts[3] == "" {
		writeError(w, http.StatusBadRequest, "missing match id in URL")
		return
	}
	matchID, err := strconv.Atoi(parts[3])
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid match id")
		return
	}

	var m Match
	err = db.QueryRow(`
		SELECT id, api_id, home_team, away_team, match_date,
		       home_score, away_score, status, competition, updated_at
		FROM matches WHERE id = ?`, matchID).Scan(
		&m.ID, &m.ApiID, &m.HomeTeam, &m.AwayTeam, &m.MatchDate,
		&m.HomeScore, &m.AwayScore, &m.Status, &m.Competition, &m.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "match not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}

	if m.Status == "SCHEDULED" {
		parsedDate, err := time.Parse("2006-01-02 15:04:05", m.MatchDate)
		if err == nil && time.Now().UTC().After(parsedDate) {
			checkFn(db, m.ApiID, matchID)
			db.QueryRow(`
				SELECT id, api_id, home_team, away_team, match_date,
				       home_score, away_score, status, competition, updated_at
				FROM matches WHERE id = ?`, matchID).Scan(
				&m.ID, &m.ApiID, &m.HomeTeam, &m.AwayTeam, &m.MatchDate,
				&m.HomeScore, &m.AwayScore, &m.Status, &m.Competition, &m.UpdatedAt,
			)
		}
	}

	writeJSON(w, http.StatusOK, m)
}

func InsertTestMatches(db *sql.DB) {
	var count int
	db.QueryRow(`SELECT COUNT(*) FROM matches WHERE api_id > 10000`).Scan(&count)
	//s'il y a deja de vrais matchs API -> pas besoin des matchs de test
	if count > 0 {
		return
	}
	now := time.Now().UTC()
	testMatches := []struct {
		apiID       int
		home, away  string
		date        time.Time
		competition string
	}{
		{1001, "PSG", "Olympique de Marseille", now.Add(24 * time.Hour), "FL1"},
		{1002, "Olympique Lyonnais", "AS Monaco", now.Add(48 * time.Hour), "FL1"},
		{1003, "Arsenal", "Chelsea", now.Add(72 * time.Hour), "PL"},
		{1004, "Real Madrid", "FC Barcelona", now.Add(96 * time.Hour), "PD"},
		{1005, "Bayern Munich", "Borussia Dortmund", now.Add(120 * time.Hour), "BL1"},
	}
	for _, tm := range testMatches {
		db.Exec(`
			INSERT OR IGNORE INTO matches
			(api_id, home_team, away_team, match_date, status, competition)
			VALUES (?, ?, ?, ?, 'SCHEDULED', ?)`,
			tm.apiID, tm.home, tm.away,
			tm.date.Format("2006-01-02 15:04:05"),
			tm.competition)
	}
}

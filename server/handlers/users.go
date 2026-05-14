package handlers

import (
	"database/sql"
	"net/http"
	"strconv"
	"strings"
)

// GetLeaderboard gère GET /api/leaderboard
func GetLeaderboard(db *sql.DB, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	rows, err := db.Query(`
	SELECT id, username, points_total
	FROM users
	ORDER BY points_total DESC
	LIMIT 50`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	defer rows.Close()

	type RankEntry struct {
		Rank     int    `json:"rank"`
		UserID   int    `json:"user_id"`
		Username string `json:"username"`
		Points   int    `json:"points"`
	}

	global := []RankEntry{}
	rank := 1
	for rows.Next() {
		var e RankEntry
		if err := rows.Scan(&e.UserID, &e.Username, &e.Points); err != nil {
			writeError(w, http.StatusInternalServerError, "scan error")
			return
		}
		e.Rank = rank
		global = append(global, e)
		rank++
	}

	weekRows, err := db.Query(`
	SELECT u.id, u.username, COALESCE(SUM(p.points_earned), 0) as weekly_points
	FROM users u
	LEFT JOIN predictions p ON p.user_id = u.id
	AND p.created_at >= datetime('now', '-7 days')
	AND p.points_earned IS NOT NULL
	GROUP BY u.id, u.username
	ORDER BY weekly_points DESC
	LIMIT 50`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	defer weekRows.Close()

	weekly := []RankEntry{}
	rank = 1
	for weekRows.Next() {
		var e RankEntry
		if err := weekRows.Scan(&e.UserID, &e.Username, &e.Points); err != nil {
			writeError(w, http.StatusInternalServerError, "scan error")
			return
		}
		e.Rank = rank
		weekly = append(weekly, e)
		rank++
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"global": global,
		"weekly": weekly,
	})
}

// GetUserByID gère GET /api/users/{id}
func GetUserByID(db *sql.DB, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 4 || parts[3] == "" || parts[3] == "me" {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}
	userID, err := strconv.Atoi(parts[3])
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}

	writeUserProfile(db, w, userID)
}

// GetMe gère GET /api/users/me
func GetMe(db *sql.DB, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	userID, _, err := ValidateJWT(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	writeUserProfile(db, w, userID)
}

// DeleteMe gère DELETE /api/users/me
func DeleteMe(db *sql.DB, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	userID, _, err := ValidateJWT(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	tx, err := db.Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}

	_, err = tx.Exec(`DELETE FROM comments WHERE user_id = ?`, userID)
	if err != nil {
		tx.Rollback()
		writeError(w, http.StatusInternalServerError, "failed to delete comments")
		return
	}

	_, err = tx.Exec(`DELETE FROM predictions WHERE user_id = ?`, userID)
	if err != nil {
		tx.Rollback()
		writeError(w, http.StatusInternalServerError, "failed to delete predictions")
		return
	}

	result, err := tx.Exec(`DELETE FROM users WHERE id = ?`, userID)
	if err != nil {
		tx.Rollback()
		writeError(w, http.StatusInternalServerError, "failed to delete user")
		return
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		tx.Rollback()
		writeError(w, http.StatusNotFound, "user not found")
		return
	}

	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "transaction error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "account deleted successfully",
	})
}

// writeUserProfile construit et envoie le profil complet d'un utilisateur
func writeUserProfile(db *sql.DB, w http.ResponseWriter, userID int) {
	var username, email, createdAt string
	var pointsTotal int
	err := db.QueryRow(`
	SELECT username, email, points_total, created_at
	FROM users WHERE id = ?
	`, userID).Scan(&username, &email, &pointsTotal, &createdAt)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}

	// ✅ Stats complètes : total, corrects, ratés, en attente
	var totalPreds, correctPreds, wrongPreds, pendingPreds int
	db.QueryRow(`
		SELECT
			COUNT(*),
			COALESCE(SUM(CASE WHEN points_earned > 0 THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN points_earned IS NOT NULL AND points_earned = 0 THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN points_earned IS NULL THEN 1 ELSE 0 END), 0)
		FROM predictions
		WHERE user_id = ?
	`, userID).Scan(&totalPreds, &correctPreds, &wrongPreds, &pendingPreds)

	successRate := 0.0
	evaluated := totalPreds - pendingPreds
	if evaluated > 0 {
		successRate = float64(correctPreds) / float64(evaluated) * 100
	}

	// Historique des 10 derniers pronostics
	rows, err := db.Query(`
		SELECT p.home_score_pred, p.away_score_pred, p.points_earned,
		       m.home_team, m.away_team, m.match_date,
		       m.home_score, m.away_score
		FROM predictions p
		JOIN matches m ON m.id = p.match_id
		WHERE p.user_id = ?
		ORDER BY p.created_at DESC
		LIMIT 10
	`, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	defer rows.Close()

	type HistoryEntry struct {
		HomeTeam     string `json:"home_team"`
		AwayTeam     string `json:"away_team"`
		MatchDate    string `json:"match_date"`
		HomePred     int    `json:"home_score_pred"`
		AwayPred     int    `json:"away_score_pred"`
		HomeReal     *int   `json:"home_score"`
		AwayReal     *int   `json:"away_score"`
		PointsEarned *int   `json:"points_earned"`
	}

	history := []HistoryEntry{}
	for rows.Next() {
		var h HistoryEntry
		rows.Scan(
			&h.HomePred, &h.AwayPred, &h.PointsEarned,
			&h.HomeTeam, &h.AwayTeam, &h.MatchDate,
			&h.HomeReal, &h.AwayReal,
		)
		history = append(history, h)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"user_id":      userID,
		"username":     username,
		"email":        email,
		"points_total": pointsTotal,
		"stats": map[string]interface{}{
			"total_predictions": totalPreds,
			"correct":           correctPreds,
			"wrong":             wrongPreds,
			"pending":           pendingPreds,
			"success_rate":      successRate,
		},
		"history":    history,
		"created_at": createdAt,
	})
}

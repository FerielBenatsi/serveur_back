package handlers

import (
	"database/sql"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Prediction represente un pronostic en base de données
type Prediction struct {
	ID            int    `json:"prediction_id"`
	UserID        int    `json:"user_id"`
	MatchID       int    `json:"match_id"`
	HomeScorePred int    `json:"home_score_pred"`
	AwayScorePred int    `json:"away_score_pred"`
	PointsEarned  *int   `json:"points_earned"`
	CreatedAt     string `json:"created_at"`
}

// parseDate essaie plusieurs formats de date
func parseDate(dateStr string) (time.Time, error) {
	formats := []string{
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05-07:00",
		time.RFC3339,
	}
	for _, f := range formats {
		if t, err := time.Parse(f, dateStr); err == nil {
			return t, nil
		}
	}
	return time.Time{}, sql.ErrNoRows
}

// PostPrediction gère POST /api/matches/{id}/predictions
func PostPrediction(db *sql.DB, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	userID, _, err := ValidateJWT(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	matchID, err := extractMatchID(r.URL.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid match id")
		return
	}

	var body struct {
		HomeScorePred int `json:"home_score_pred"`
		AwayScorePred int `json:"away_score_pred"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if body.HomeScorePred < 0 || body.AwayScorePred < 0 {
		writeError(w, http.StatusBadRequest, "scores cannot be negative")
		return
	}

	var matchDate string
	var status string
	err = db.QueryRow(
		`SELECT match_date, status FROM matches WHERE id = ?`, matchID,
	).Scan(&matchDate, &status)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "match not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}

	if status == "IN_PLAY" || status == "FINISHED" {
		writeError(w, http.StatusForbidden, "match already started, prediction not allowed")
		return
	}

	parsedDate, err := parseDate(matchDate)
	if err == nil && time.Now().UTC().After(parsedDate) {
		writeError(w, http.StatusForbidden, "match already started, prediction not allowed")
		return
	}

	result, err := db.Exec(`
	INSERT INTO predictions(user_id, match_id, home_score_pred, away_score_pred)
	VALUES(?, ?, ?, ?)`, userID, matchID, body.HomeScorePred, body.AwayScorePred)
	if err != nil {
		writeError(w, http.StatusConflict, "prediction already submitted for this match")
		return
	}

	predID, _ := result.LastInsertId()
	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"prediction_id":   predID,
		"match_id":        matchID,
		"home_score_pred": body.HomeScorePred,
		"away_score_pred": body.AwayScorePred,
		"points_earned":   nil,
		"created_at":      time.Now().UTC().Format("2006-01-02T15:04:05Z"),
	})
}

// PutPrediction gère PUT /api/matches/{id}/predictions
// Modifie le pronostic existant — uniquement si le match commence dans 24h ou plus
func PutPrediction(db *sql.DB, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	userID, _, err := ValidateJWT(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	matchID, err := extractMatchID(r.URL.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid match id")
		return
	}

	var body struct {
		HomeScorePred int `json:"home_score_pred"`
		AwayScorePred int `json:"away_score_pred"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if body.HomeScorePred < 0 || body.AwayScorePred < 0 {
		writeError(w, http.StatusBadRequest, "scores cannot be negative")
		return
	}

	var matchDate string
	var status string
	err = db.QueryRow(
		`SELECT match_date, status FROM matches WHERE id = ?`, matchID,
	).Scan(&matchDate, &status)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "match not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}

	if status == "IN_PLAY" || status == "FINISHED" {
		writeError(w, http.StatusForbidden, "match already started, cannot modify prediction")
		return
	}

	// ✅ Parse avec plusieurs formats de date
	parsedDate, parseErr := parseDate(matchDate)
	if parseErr != nil {
		writeError(w, http.StatusInternalServerError, "invalid match date")
		return
	}

	// Vérifie qu'il reste au moins 24h avant le match
	timeUntilMatch := parsedDate.Sub(time.Now().UTC())
	if timeUntilMatch < 24*time.Hour {
		writeError(w, http.StatusForbidden, "cannot modify prediction less than 24 hours before match")
		return
	}

	// Vérifie que l'utilisateur a bien un pronostic existant
	var predID int
	err = db.QueryRow(
		`SELECT id FROM predictions WHERE user_id = ? AND match_id = ?`,
		userID, matchID,
	).Scan(&predID)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "no prediction found for this match")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}

	// Met à jour le pronostic
	_, err = db.Exec(
		`UPDATE predictions SET home_score_pred = ?, away_score_pred = ? WHERE id = ?`,
		body.HomeScorePred, body.AwayScorePred, predID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update prediction")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"prediction_id":   predID,
		"match_id":        matchID,
		"home_score_pred": body.HomeScorePred,
		"away_score_pred": body.AwayScorePred,
		"points_earned":   nil,
		"message":         "prediction updated successfully",
	})
}

// GetPredictions gère GET /api/matches/{id}/predictions
func GetPredictions(db *sql.DB, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	matchID, err := extractMatchID(r.URL.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid match id")
		return
	}

	var status string
	err = db.QueryRow(`SELECT status FROM matches WHERE id = ?`, matchID).Scan(&status)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "match not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}

	orderBy := "p.created_at DESC"
	if status == "FINISHED" {
		orderBy = "p.points_earned DESC, p.created_at ASC"
	}

	rows, err := db.Query(`
	SELECT p.id, p.user_id, p.match_id,
	       p.home_score_pred, p.away_score_pred,
	       p.points_earned, p.created_at,
	       u.username
	FROM predictions p
	JOIN users u ON u.id = p.user_id
	WHERE p.match_id = ?
	ORDER BY `+orderBy, matchID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	defer rows.Close()

	type PredWithUser struct {
		Prediction
		Username string `json:"username"`
	}

	predictions := []PredWithUser{}
	for rows.Next() {
		var p PredWithUser
		err := rows.Scan(
			&p.ID, &p.UserID, &p.MatchID,
			&p.HomeScorePred, &p.AwayScorePred,
			&p.PointsEarned, &p.CreatedAt,
			&p.Username,
		)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "scan error")
			return
		}
		predictions = append(predictions, p)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"match_id":     matchID,
		"predictions":  predictions,
		"count":        len(predictions),
		"match_status": status,
	})
}

// CalculatePoints calcule et enregistre les points pour tous les pronostics d'un match terminé
func CalculatePoints(db *sql.DB, matchID, homeScore, awayScore int) error {
	rows, err := db.Query(`
		SELECT id, user_id, home_score_pred, away_score_pred
		FROM predictions
		WHERE match_id = ? AND points_earned IS NULL
	`, matchID)
	if err != nil {
		return err
	}
	defer rows.Close()

	type pred struct {
		id, userID, homePred, awayPred int
	}
	var preds []pred
	for rows.Next() {
		var p pred
		if err := rows.Scan(&p.id, &p.userID, &p.homePred, &p.awayPred); err != nil {
			return err
		}
		preds = append(preds, p)
	}
	rows.Close()

	tx, err := db.Begin()
	if err != nil {
		return err
	}

	for _, p := range preds {
		points := computePoints(p.homePred, p.awayPred, homeScore, awayScore)

		_, err1 := tx.Exec(
			`UPDATE predictions SET points_earned = ? WHERE id = ?`,
			points, p.id,
		)
		_, err2 := tx.Exec(
			`UPDATE users SET points_total = points_total + ? WHERE id = ?`,
			points, p.userID,
		)

		if err1 != nil || err2 != nil {
			tx.Rollback()
			return err1
		}
	}

	return tx.Commit()
}

// computePoints applique la règle de scoring
func computePoints(homePred, awayPred, homeReal, awayReal int) int {
	if homePred == homeReal && awayPred == awayReal {
		return 3
	}
	predResult := sign(homePred - awayPred)
	realResult := sign(homeReal - awayReal)
	if predResult == realResult {
		return 1
	}
	return 0
}

// sign retourne -1, 0 ou 1 selon le signe d'un entier
func sign(n int) int {
	if n > 0 {
		return 1
	}
	if n < 0 {
		return -1
	}
	return 0
}

// extractMatchID extrait l'id du match depuis une URL
func extractMatchID(path string) (int, error) {
	parts := strings.Split(path, "/")
	if len(parts) < 4 {
		return 0, sql.ErrNoRows
	}
	return strconv.Atoi(parts[3])
}

package handlers

import(
	"database/sql"
	"net/http"
	"strconv"
	"strings"
)


//GetLeaderboard gere GET /api/leaderboard
//Retourne le classement general trie par points
func GetLeaderboard(db *sql.DB, w http.ResponseWriter, r *http.Request){
	if r.Method != http.MethodGet{
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	//Classement general - tous les utilisateurs tries par points
	rows, err := db.Query(`
	SELECT id, username, points_total
	FROM users
	ORDER BY points_total DESC 
	LIMIT 50`)
	if err != nil{
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	defer rows.Close()
	type RankEntry struct{
		Rank          int     `json:"rank"`
		UserID        int     `json:"user_id"`
		Username      string  `json:"username"`
		Points        int     `json:"points"`
	}
	global := []RankEntry{}
	rank := 1
	for rows.Next(){
		var e RankEntry
		if err := rows.Scan(&e.UserID, &e.Username, &e.Points); err != nil {
    writeError(w, http.StatusInternalServerError, "scan error")
    return
}
		e.Rank = rank
		global = append(global,e)
		rank++
	}
	//Classement hebdomadaire - points gagnés cette semaine uniquement
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
// Retourne le profil public d'un utilisateur avec ses statistiques
func GetUserByID(db *sql.DB, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}


	// Extraction de l'id depuis l'URL : /api/users/3
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
// Retourne le profil de l'utilisateur connecté — nécessite un token JWT
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

//writeUserProfile construit et envoie le profil complet d'un utilisateur
func writeUserProfile(db *sql.DB, w http.ResponseWriter, userID int){
	//info de base 
	var username, email, createdAt string
	var pointsTotal int
	err := db.QueryRow(`
	SELECT username, email, points_total, created_at
	FROM users WHERE id=?
	`, userID).Scan(&username, &email, &pointsTotal, &createdAt)
	if err == sql.ErrNoRows{
		writeError(w, http.StatusNotFound, "user not found")
		return 
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}


// Statistiques : nombre total de pronostics et taux de réussite
	var totalPreds, correctPreds int
	db.QueryRow(`
		SELECT COUNT(*), COALESCE(SUM(CASE WHEN points_earned > 0 THEN 1 ELSE 0 END), 0)
		FROM predictions
		WHERE user_id = ? AND points_earned IS NOT NULL
	`, userID).Scan(&totalPreds, &correctPreds)

	successRate := 0.0
	if totalPreds > 0 {
		successRate = float64(correctPreds) / float64(totalPreds) * 100
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
		HomeTeam      string `json:"home_team"`
		AwayTeam      string `json:"away_team"`
		MatchDate     string `json:"match_date"`
		HomePred      int    `json:"home_score_pred"`
		AwayPred      int    `json:"away_score_pred"`
		HomeReal      *int   `json:"home_score"`
		AwayReal      *int   `json:"away_score"`
		PointsEarned  *int   `json:"points_earned"`
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
			"success_rate":      successRate,
		},
		"history":    history,
		"created_at": createdAt,
	})
}





































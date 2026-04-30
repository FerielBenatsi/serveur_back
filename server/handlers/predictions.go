package handlers

import(
	"database/sql"
	"net/http"
	"strconv"
	"strings"
	"time"
)

//Prediction represente un pronostic en base de donnees
type Prediction struct{
	ID          int    `json:"prediction_id"`
	UserID      int    `json:"user_id"`
	MatchID     int    `json:"match_id"`
	HomeScorePred int  `json:"home_score_pred"`
	AwayScorePred int  `json:"away_score_pred"`
	PointsEarned  *int `json:"points_earned"`
	CreatedAt     string `json:"created_at"`
}

//PostPrediction gere POST /api/matches/{id}/predictions
//Soumet un pronostic pour un match - necessite un token JWT valide

func PostPrediction(db *sql.DB, w http.ResponseWriter, r *http.Request){
	if r.Method != http.MethodPost{
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	//verification du token JWT 
	userID, _, err := ValidateJWT(r)
	if err != nil{
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	//extraction de l'id du match depuis l'URL : /api/matches/42/predictions
	matchID, err := extractMatchID(r.URL.Path)
	if err != nil{
		writeError(w, http.StatusBadRequest, "invalid match id")
		return 
	}
	//lecture du corps JSON(recevoir les donnees envoyees par le client(json))
	//je prepare un objet pour lire le json du client 
	var body struct{
		HomeScorePred int `json:"home_score_pred"`
		AwayScorePred int `json:"away_score_pred"`

	}
	if err := decodeJSON(r, &body); err != nil{
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return

	}

	// scores ne peuvent pas être négatifs
if body.HomeScorePred < 0 || body.AwayScorePred < 0 {
    writeError(w, http.StatusBadRequest, "scores cannot be negative")
    return
}
	//verification que le match existe et n'a pas encore commencé
	var matchDate string
	var status string
	err = db.QueryRow(
		`SELECT match_date, status FROM matches WHERE id = ?`, matchID,).Scan(&matchDate, &status)
		if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "match not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	//On refuse le pronostic si le match est deja commencé ou términé
	if status == "IN_PLAY" || status == "FINISHED"{
		writeError(w, http.StatusForbidden,"match already started, prediction not allowed")
		return
	}
	//on verifie aussi la date au cas ou le statut n'est pas encore mis a jour
	parsedDate, err := time.Parse("2006-01-02 15:04:05", matchDate)
	if err == nil && time.Now().UTC().After(parsedDate) {
		writeError(w, http.StatusForbidden, "match already started, prediction not allowed")
		return
	}
	//insertion du pronostic - unique(user_id, match_id) empeche les doublons
	result, err := db.Exec(`
	INSERT INTO predictions(user_id, match_id, home_score_pred, away_score_pred)
	VALUES(?, ?, ?, ?) `, userID, matchID, body.HomeScorePred, body.AwayScorePred)
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

//GetPredictions gere GET /api/matches/{id}/predictions
//Retourne les pronostics d'un match (seulement si le maatch est terminé)
func GetPredictions(db *sql.DB, w http.ResponseWriter, r *http.Request){
	if r.Method != http.MethodGet{
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	matchID, err := extractMatchID(r.URL.Path)
	if err != nil{
		writeError(w, http.StatusBadRequest, "invalid match id")
		return
	}
	//on verifie que le match est bien terminé avant d'exposer les pronostics
	var status string
	err = db.QueryRow(`SELECT status FROM matches WHERE id = ?`, matchID).Scan(&status)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "match not found")
		return
	}
	if status != "FINISHED" {
		writeError(w, http.StatusForbidden, "predictions are only visible after the match")
		return
	}
	rows, err := db.Query(`
	SELECT p.id, p.user_id, p.match_id,
	p.home_score_pred, p.away_score_pred,
	p.points_earned, p.created_at,
	u.username
	FROM predictions p
	JOIN users u ON u.id= p.user_id
	WHERE p.match_id=?
	ORDER BY p.points_earned DESC`, matchID)
	if err != nil{
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	defer rows.Close()

	type PredWithUser struct{
		Prediction
		Username string `json:"username"`

	}
	predictions := []PredWithUser{}
	for rows.Next(){
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
		"match_id": matchID,
		"predictions": predictions,
		"count": len(predictions),
	})

}


//CalculatePoints calcule et enregistre les points pour tous les pronostics d'un match terminé
//cette fct est appelée par la goroutine planifiée (etape7)
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
    rows.Close() // fermer avant de commencer la transaction

    // Transaction : si une mise à jour échoue, tout est annulé
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
            tx.Rollback() // annule tout si erreur
            return err1
        }
    }

    return tx.Commit()
}
//computePoints applique la regle de scoring du dossier TME6
func computePoints(homePred, awayPred, homeReal, awayReal int)int{
	//score exact -> 3 points
	if homePred == homeReal && awayPred == awayReal{
		return 3
	}
	//Bon vainqueur ou bon nul -> 1 point
	predResult := sign(homePred - awayPred)
	realResult := sign(homeReal - awayReal)
	if predResult == realResult{
		return 1
	}
	return 0
}


//sign retourne -1, 0 ou 1 selon le signe d'un entier
func sign(n int )int{
	if n > 0 {
		return 1
	}
	if n < 0{
		return -1
	}
	return 0
}

//extractMatchID extrait l'id du match depuis une URL de la forme /api/matches/{id}/....
func extractMatchID(path string)(int, error){
	parts := strings.Split(path, "/")
	// /api/matches/42/predictions -> parts = ["", "api", "matches", "42", "predictions"]
	if len(parts) < 4{
		return 0, sql.ErrNoRows
	}
	return strconv.Atoi(parts[3])
}











































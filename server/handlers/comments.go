package handlers
import (
	"database/sql"
	"net/http"
	"time"
)
//comment represente un commentaire en base de données
type Comment struct{
	ID             int      `json:"comment_id"`
	UserID         int      `json:"user_id"`
	MatchID        int      `json:"match_id"`
	Username       string   `json:"username"`
	Content        string   `json:"content"`
	CreatedAt       string  `json:"created_at"`
}

//GetComments gere GET /api/matches/{id}/comments
//Retourne tous les commentaires d'un match, du plus récent au ancien
func GetComments(db *sql.DB, w http.ResponseWriter, r *http.Request){
	if r.Method != http.MethodGet{
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	matchID, err := extractMatchID(r.URL.Path)
	if err != nil{
		writeError(w, http.StatusBadRequest, "invalid match id")
		return
	}

	//On verifie que le match existe
	var exists int
	db.QueryRow(`SELECT COUNT(*) FROM matches WHERE id = ?`, matchID).Scan(&exists)
	if exists == 0{
		writeError(w, http.StatusNotFound, "match not found")
		return
	}
	rows, err := db.Query(`
	SELECT c.id, c.user_id, c.match_id, u.username, c.content, c.created_at
	FROM comments c
	JOIN users u ON u.id = c.user_id
	WHERE c.match_id = ?
	ORDER BY c.created_at DESC`, matchID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	defer rows.Close()

	comments := []Comment{}
	for rows.Next(){
		var c Comment
		err := rows.Scan(
			&c.ID, &c.UserID, &c.MatchID,
			&c.Username, &c.Content, &c.CreatedAt,
		)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "scan error")
			return
		}
		comments = append(comments, c)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"match_id": matchID,
		"comments": comments,
		"count": len(comments),
	})

}




// PostComment gère POST /api/matches/{id}/comments
// Publie un commentaire sous un match — nécessite un token JWT valide
func PostComment(db *sql.DB, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Vérification du token JWT
	userID, username, err := ValidateJWT(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	matchID, err := extractMatchID(r.URL.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid match id")
		return
	}

	// Vérification que le match existe
	var exists int
	db.QueryRow(`SELECT COUNT(*) FROM matches WHERE id = ?`, matchID).Scan(&exists)
	if exists == 0 {
		writeError(w, http.StatusNotFound, "match not found")
		return
	}

	// Lecture du contenu du commentaire
	var body struct {
		Content string `json:"content"`
	}
	if err := decodeJSON(r, &body); err != nil || body.Content == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}

	// Insertion en BDD
	result, err := db.Exec(`
		INSERT INTO comments (user_id, match_id, content)
		VALUES (?, ?, ?)
	`, userID, matchID, body.Content)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}

	commentID, _ := result.LastInsertId()

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"comment_id": commentID,
		"match_id":   matchID,
		"user_id":    userID,
		"username":   username,
		"content":    body.Content,
		"created_at": time.Now().UTC().Format("2006-01-02T15:04:05Z"),
	})
}









package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

// clé secrète pour signer les tokens JWT
// en production elle serait dans une variable d'environnement
const jwtSecret = "sportpulse-secret-key-2026"

// register gere POST /api/auth/register
// il crée un nouveau compte utilisateur
func Register(db *sql.DB, w http.ResponseWriter, r *http.Request) {
	//On accepte uniquement les requetes POST
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed ")
		return
	}

	//On lit le corps de la requete JSON
	var body struct {
		Username string `json:"username"`
		Email    string `json:"email"`
		Password string `json:"password"`
	}

	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	//verification que les champs ne sont pas vides
	if body.Username == "" || body.Email == "" || body.Password == "" {
		writeError(w, http.StatusBadRequest, "username, email and password are required")
		return
	}

	//On hash le mot de passe avec bcrypt avant de le stocker
	hash, err := bcrypt.GenerateFromPassword([]byte(body.Password), bcrypt.DefaultCost)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to hash password")
		return
	}

	//On insere l'utilisateur dans la base de données
	result, err := db.Exec(
		`INSERT INTO users (username, email, password_hash) VALUES (?, ?, ?)`, body.Username, body.Email, string(hash),
	)

	if err != nil {
		//si l'username ou l'email existe deja -> erreur 409 conflict
		writeError(w, http.StatusConflict, "username or email already taken")
		return
	}

	//on recupere l'id du nouvel utilisateur
	userID, _ := result.LastInsertId()

	//on repond avec les infos du compte créé (sans le mot de passe!)
	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"user_id":  userID,
		"username": body.Username,
		"email":    body.Email,
		"message":  "account created successfully",
	})

}

// Login gere POST /api/auth/login
// il vérifie les identifiants et retourne un token JWT
func Login(db *sql.DB, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed ")
		return
	}
	//on lit email + password depuis le corps JSON
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if body.Email == "" || body.Password == "" {
		writeError(w, http.StatusBadRequest, "email and password are required")
		return
	}

	//on cherche l'utilisateur par email dans la BDD
	var userID int
	var username, passwordHash string
	err := db.QueryRow(
		`SELECT id, username, password_hash FROM users WHERE email=?`,
		body.Email,
	).Scan(&userID, &username, &passwordHash)

	if err == sql.ErrNoRows {
		//Email inconnu -> on dit juste "idents incorrects (securité)"
		writeError(w, http.StatusUnauthorized, "invalid email or password ")
		return
	}

	if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}

	//On compare le mot de passe saisi avec le hash stocké
	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(body.Password)); err != nil {
		writeError(w, http.StatusUnauthorized, "invalid email or password")
		return
	}

	//Mot de passe correct -> on genere un token JWT valable 24h
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id":  userID,
		"username": username,
		"exp":      time.Now().Add(24 * time.Hour).Unix(),
	})

	tokenString, err := token.SignedString([]byte(jwtSecret))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}

	// On retourne le token au client
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"token":    tokenString,
		"user_id":  userID,
		"username": username,
	})
}

// ValidateJWT vérifie un token JWT et retourne le user_id
// Cette fonction sera utilisée par les autres handlers pour protéger les routes
func ValidateJWT(r *http.Request) (int, string, error) {
	//on lit le header Authorization: Bearer <token>
	authHeader := r.Header.Get("Authorization")
	if len(authHeader) < 8 || authHeader[:7] != "Bearer " {
		return 0, "", jwt.ErrSignatureInvalid
	}
	tokenString := authHeader[7:]

	//on parse et vérifie le token
	token, err := jwt.Parse(tokenString, func(t *jwt.Token) (interface{}, error) {
		return []byte(jwtSecret), nil
	})
	if err != nil || !token.Valid {
		return 0, "", err
	}

	//on extrait user_id et username des claims
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return 0, "", jwt.ErrSignatureInvalid
	}
	userID := int(claims["user_id"].(float64))
	username := claims["username"].(string)
	return userID, username, nil

}

//__________helpers____

// writeJSON envoie une reponse JSON avec le code HTTP donné
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)

}

// writeError envoie une reponse JSON uniforme
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}


//decodeJSON lit et decode le corps JSON d'une requete
//trans du JSON envoyé par le client en variables GO

func decodeJSON(r *http.Request, dst interface{}) error {
	return json.NewDecoder(r.Body).Decode(dst)
}


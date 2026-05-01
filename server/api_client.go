package main

//1. recupere les matchs: API-> JSON -> GO
//2. stocker en base: insert or ignore
//3. mettre a jour resultats + points: update matches, calculatePoints()

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sportpulse/server/handlers"
	"time"
)

// J’utilise une variable d’environnement pour sécuriser la clé API
// remplace par la vraie clé API football-data.org
// const footballAPIKey = " 48a102598bc74402978d6c16c458d162"
const footballAPIBase = "https://api.football-data.org/v4"

// competitions qu'on suit - toutes disponibles sur le plan gratuit
var competitions = []string{"FL1", "PL", "PD", "BL1", "SA", "CL"}

//var competitions = []string{"FL1"}

func getAPIKey() string {
	key := os.Getenv("FOOTBALL_API_KEY")
	if key == "" {
		log.Fatal("FOOTBALL_API_KEY environment variable not set")
	}
	return key //"48a102598bc74402978d6c16c458d162" // key
}

// ------structures pour parser la reponse JSON de l'API------
type apiResponse struct {
	Matches []apiMatch `json:"matches"`
}

type apiMatch struct {
	ID          int            `json:"id"`
	UTCDate     string         `json:"utcDate"`
	Status      string         `json:"status"`
	HomeTeam    apiTeam        `json:"homeTeam"`
	AwayTeam    apiTeam        `json:"awayTeam"`
	Score       apiScore       `json:"score"`
	Competition apiCompetition `json:"competition"`
}

type apiTeam struct {
	Name string `json:"name"`
}

type apiScore struct {
	FullTime apiGoals `json:"fullTime"`
}

type apiGoals struct {
	Home *int `json:"home"`
	Away *int `json:"away"`
}

type apiCompetition struct {
	Code string `json:"code"`
}

// ------Fonctions principales---------------------
// recupere les matchs programmés dans les 7 prochains jours
// et les insére en base de données
func FetchAndStoreScheduled() {
	log.Println("[API] Fetching scheduled matches...")
	for _, comp := range competitions {
		//on recupere les matchs dans les 7 prochains jours
		url := fmt.Sprintf(
			"%s/competitions/%s/matches?status=SCHEDULED&dateFrom=%s&dateTo=%s",
			footballAPIBase, comp, time.Now().UTC().Format("2006-01-02"),
			time.Now().UTC().Add(7*24*time.Hour).Format("2006-01-02"),
		)
		matches, err := callAPI(url) //result: liste de matchs
		if err != nil {
			log.Printf("[API] Error fetching scheduled for %s: %v\n", comp, err)
			continue
		}
		inserted := 0 //pour compter combien de matchs ajoutés
		for _, m := range matches {
			//on parse la date de l'api (format ISO 8601)
			parsedDate, err := time.Parse(time.RFC3339, m.UTCDate)
			if err != nil {
				continue
			}
			//insert or ignore : on n'insere que si le match n'existe pas deja
			_, err = DB.Exec(`
			INSERT OR IGNORE INTO matches 
			(api_id, home_team, away_team, match_date, status, competition)
			VALUES (?, ?, ?, ?, ?,?)`,
				m.ID,
				m.HomeTeam.Name,
				m.AwayTeam.Name,
				parsedDate.UTC().Format("2006-01-02 15:04:05"),
				m.Status,
				comp)
			if err != nil {
				log.Printf("[API] Insert error for match %d: %v\n", m.ID, err)
				continue
			}
			inserted++

		}
		log.Printf("[API] %s -> %d new matches inserted\n", comp, inserted)
		//on respecte la limite de 10 requetes/minute du plan gratuit
		time.Sleep(6 * time.Second) //on attend 6 secondes entre chaque appel

	}
}

// recupere les matchs terminés d'hier
// met a jour les scores en BDD et déclenche le calcul des points
func FetchAndStoreFinished() {
	log.Println("[API] Fetching finished matches...")
	//on va chercher les matchs terminés entre hier et aujourd'hui
	yesterday := time.Now().UTC().Add(-24 * time.Hour).Format("2006-01-02")
	today := time.Now().UTC().Format("2006-01-02")
	for _, comp := range competitions {
		url := fmt.Sprintf(
			//on demande : matchs terminés , sur 24h
			"%s/competitions/%s/matches?status=FINISHED&dateFrom=%s&dateTo=%s",
			footballAPIBase,
			comp,
			yesterday,
			today,
		)
		matches, err := callAPI(url) //recupere les matchs terminés
		if err != nil {
			log.Printf("[API] Error fetching finished for %s: %v\n", comp, err)
			continue
		}
		for _, m := range matches {
			//on ne traite que les matchs avec un score disponible
			if m.Score.FullTime.Home == nil || m.Score.FullTime.Away == nil {
				continue
			}
			//recuperer scores
			homeScore := *m.Score.FullTime.Home
			awayScore := *m.Score.FullTime.Away
			//on met a jour le score et le statut dans la table matches
			result, err := DB.Exec(`
			UPDATE matches
			SET home_score = ?,
			away_score = ?,
			status = 'FINISHED',
			updated_at = CURRENT_TIMESTAMP
			WHERE api_id = ? AND status != 'FINISHED'
			`, homeScore, awayScore, m.ID)
			if err != nil {
				log.Printf("[API] Update error for match %d: %v\n", m.ID, err)
				continue
			}
			//on verifier si une ligne a vraiment été modifiée
			rowsAffected, _ := result.RowsAffected()
			if rowsAffected == 0 {
				//match deja mis a jour ou pas en BDD - on passe
				continue
			}
			//on recupere l'id interne du match pour CalculatePoints
			var internalID int
			err = DB.QueryRow(
				`SELECT id FROM matches WHERE api_id = ?`, m.ID,
			).Scan(&internalID)
			if err != nil {
				continue
			}
			//on calcule les points de tous les utilisateurs ayant pronostiqué ce match
			err = handlers.CalculatePoints(DB, internalID, homeScore, awayScore)
			if err != nil {
				log.Printf("[API] CalculatePoints error for match %d: %v\n", internalID, err)
			} else {
				log.Printf("[API] Points calculated for match %d (%s %d-%d %s)\n",
					internalID, m.HomeTeam.Name, homeScore, awayScore, m.AwayTeam.Name)

			}
		}

		time.Sleep(6 * time.Second)
	}

}

// interroge l'API pour un match précis
// utilisé quand un utilisateur consulte un match dont la date est passée
// mais dont le statut en BDD est encore SCHEDULED
// plusieurs raisons: api pas appelée a temps, serveur arreté, bug, delai
// corriger les données en retard
// synchroniser API<->DB
func FetchMatchByApiID(apiID int) (*apiMatch, error) {
	url := fmt.Sprintf("%s/matches/%d", footballAPIBase, apiID)
	matches, err := callAPI(url)
	if err != nil {
		return nil, err
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("match %d not found in API", apiID)
	}
	return &matches[0], nil

}

// callAPI effectue un appel HTTP GET vers l'API et retourne la liste des matchs
func callAPI(url string) ([]apiMatch, error) {
	client := &http.Client{Timeout: 30 * time.Second} //creation client HTTP(cree un outil pour envoyer des requetes internet)Timeout= securité (evite blocage)
	req, err := http.NewRequest("GET", url, nil)      //creation requete (prepare une requete HTTP GET vers l'url)
	if err != nil {
		return nil, err
	}
	//header d'authentification obligatoire
	req.Header.Set("X-Auth-Token", getAPIKey()) //ajout clé API
	resp, err := client.Do(req)                 //envoi requete vers internet
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %v", err)
	}
	defer resp.Body.Close()
	//on vérifie le code HTTP retourné
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("rate limit exceeded (429)")

	}
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("invalid API key (401)")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code : %d", resp.StatusCode)

	}
	body, err := io.ReadAll(resp.Body) //lire reponse(recupere le JSON)
	if err != nil {
		return nil, err
	}
	//deux formats possibles : liste de matchs ou match unique
	//on essaie d'abord le format liste
	//convertir JSON -> struct
	var listResp apiResponse
	if err := json.Unmarshal(body, &listResp); err == nil && len(listResp.Matches) > 0 {
		return listResp.Matches, nil
	}
	//sinon on essaie le format match unique (GET /matches/{id})
	var single apiMatch
	if err := json.Unmarshal(body, &single); err == nil && single.ID != 0 {
		return []apiMatch{single}, nil
	}
	return []apiMatch{}, nil //si rien trouvé (retourne tab vide)

}

// verifie via l'API si un match précis est terminé
// et met a jour la BDD si c'est le cas
func CheckAndUpdateMatch(db *sql.DB, apiID, internalID int) {
	apiMatch, err := FetchMatchByApiID(apiID)
	if err != nil {
		log.Printf("[API] checkAndUpdateMatch error for api_id %d: %v\n", apiID, err)
		return
	}
	if apiMatch.Status != "FINISHED" {
		return
	}
	if apiMatch.Score.FullTime.Home == nil || apiMatch.Score.FullTime.Away == nil {
		return
	}
	homeScore := *apiMatch.Score.FullTime.Home
	awayScore := *apiMatch.Score.FullTime.Away
	db.Exec(`
	UPDATE matches
	SET home_score = ?, away_score = ?, status = 'FINISHED', updated_at = CURRENT_TIMESTAMP
	WHERE id = ?`, homeScore, awayScore, internalID)
	handlers.CalculatePoints(db, internalID, homeScore, awayScore)
	log.Printf("[API] On-demand update: match %d finished %d-%d\n", internalID, homeScore, awayScore)

}

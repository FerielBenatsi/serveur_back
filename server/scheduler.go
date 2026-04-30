package main

//chaque nuit: 02h00 -> recuperer matchs,
// mettre a jour scores
// calculer points --- sans intervention manuelle

import (
	"log"
	"time"
)

// StartScheduler lance les deux goroutines :
// 1. Mise à jour nocturne à 02h00
// 2. Vérification toutes les 5 minutes des matchs en cours
func StartScheduler() {
	log.Println("[Scheduler] Started")
	go runNightlyLoop()
	go runLiveLoop()
}

// runNightlyLoop — mise à jour complète chaque nuit à 02h00
func runNightlyLoop() {
	log.Println("[Scheduler] Nightly loop started")
	for {
		now := time.Now().UTC()
		next := time.Date(now.Year(), now.Month(), now.Day(),
			2, 0, 0, 0, time.UTC)
		if now.After(next) {
			next = next.Add(24 * time.Hour)
		}
		waitDuration := next.Sub(now)
		log.Printf("[Scheduler] Next nightly run in %s\n",
			waitDuration.Round(time.Minute))
		time.Sleep(waitDuration)
		runNightlyUpdate()
		time.Sleep(1 * time.Minute)
	}
}

// runLiveLoop — vérifie toutes les 5 minutes les matchs
// dont la date est passée mais le statut est encore SCHEDULED
// Ça gère le cas "pas d'internet à 02h00" et les matchs en cours
func runLiveLoop() {
	log.Println("[Scheduler] Live loop started — checking every 5 minutes")
	for {
		checkLiveMatches()
		time.Sleep(5 * time.Minute)
	}
}

// checkLiveMatches cherche en BDD les matchs dont la date est passée
// mais qui sont encore SCHEDULED, et appelle l'API pour chacun
func checkLiveMatches() {
	// On cherche les matchs dont la date est passée
	// et qui ne sont pas encore FINISHED
	rows, err := DB.Query(`
		SELECT id, api_id FROM matches
		WHERE match_date <= datetime('now')
		AND status != 'FINISHED'
		AND api_id > 10000
		LIMIT 10
	`)
	if err != nil {
		log.Printf("[Live] DB error: %v\n", err)
		return
	}
	defer rows.Close()
	//id : id de match dans la BDD : mettre a jour BDD
	//apiID : id de match dans l'API : appeler API
	type pending struct{ id, apiID int }
	//la liste des matchs à vérifier (pas encore terminés)
	var pendings []pending
	for rows.Next() {
		var p pending
		if err := rows.Scan(&p.id, &p.apiID); err != nil {
			log.Println("[Live] Scan error:", err)
			continue
		}
		pendings = append(pendings, p)
	}

	if len(pendings) == 0 {
		log.Println("[Live] No matches to update")
		return // rien à faire
	}

	log.Printf("[Live] %d match(es) to check\n", len(pendings))
	for _, p := range pendings {
		CheckAndUpdateMatch(DB, p.apiID, p.id)
		time.Sleep(6 * time.Second) // respecte rate limit API
	}
}

// runNightlyUpdate exécute les deux tâches de mise à jour nocturne
// 1. Récupérer les nouveaux matchs programmés
// 2. Récupérer les résultats et calculer les points
func runNightlyUpdate() {
	log.Println("[Scheduler] === Nightly update started ===")
	start := time.Now()

	// Tâche 1 : récupérer les matchs des 7 prochains jours
	log.Println("[Scheduler] Step 1/2 — Fetching scheduled matches...")
	FetchAndStoreScheduled()

	// Tâche 2 : récupérer les résultats et calculer les points
	log.Println("[Scheduler] Step 2/2 — Fetching finished matches & calculating points...")
	FetchAndStoreFinished()

	elapsed := time.Since(start).Round(time.Second)
	log.Printf("[Scheduler] === Nightly update finished in %s ===\n", elapsed)
}

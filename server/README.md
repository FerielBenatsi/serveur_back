# SportPulse — Serveur Go

Serveur backend du projet **SportPulse**, une plateforme de pronostics sportifs développée dans le cadre du micro-projet Web PC3R (TME6 à TME10) — M1 STL, Sorbonne Université.

---

## Description

SportPulse permet à des utilisateurs inscrits de soumettre des pronostics sur des matchs de football, de gagner des points selon la précision de leurs prédictions, et de s'affronter sur un classement général et hebdomadaire.

Le serveur est écrit en **Go** avec le package standard `net/http` (sans framework), expose une **API REST JSON**, utilise **SQLite** comme base de données locale, et se synchronise automatiquement avec l'API externe **football-data.org**.

---

## Prérequis

- Go 1.21 ou supérieur
- GCC installé (requis par le driver SQLite `mattn/go-sqlite3`)
- Un compte sur [football-data.org](https://www.football-data.org/client/register) pour obtenir une clé API gratuite

---

## Installation

```bash
# 1. Cloner le dépôt
git clone https://github.com/dalilazeghmiche/sportpulse.git
cd sportpulse/server

# 2. Installer les dépendances Go
go mod tidy
```

---

## Configuration

Crée un fichier `.env` dans le dossier `server/` :

```env
FOOTBALL_API_KEY=ta_cle_api_ici
```

> La clé API est gratuite sur [football-data.org](https://www.football-data.org/client/register).  
> Le fichier `.env` est exclu du dépôt Git via `.gitignore` — ne jamais commiter la clé.

Avant de lancer le serveur, exporte la variable d'environnement :

```bash
# Linux / macOS
export FOOTBALL_API_KEY="ta_cle_api_ici"

# Windows (PowerShell)
$env:FOOTBALL_API_KEY="ta_cle_api_ici"
```

---

## Lancement

```bash
cd sportpulse/server
go run main.go db.go api_client.go scheduler.go
```

Le serveur démarre sur **http://localhost:8091**.

Sortie attendue au démarrage :

```
2026/04/01 17:39:13 Database connected and tables ready
2026/04/01 17:39:13 [Scheduler] Started
2026/04/01 17:39:13 [Scheduler] Nightly loop started
2026/04/01 17:39:13 [Scheduler] Live loop started — checking every 5 minutes
2026/04/01 17:39:13 [Scheduler] Next nightly run in 10h21m
2026/04/01 17:39:13 SportPulse server starting on http://localhost:8091
```

---

## Structure du projet

```
server/
├── main.go              # Point d'entrée — routeur HTTP et enregistrement des routes
├── db.go                # Connexion SQLite et création des 4 tables
├── api_client.go        # Appels à l'API football-data.org
├── scheduler.go         # Goroutines planifiées (nightly 02h00 + live 5 min)
├── handlers/
│   ├── auth.go          # Register / Login (JWT + bcrypt)
│   ├── matches.go       # GET /api/matches, GET /api/matches/{id}
│   ├── predictions.go   # POST/GET /api/matches/{id}/predictions + calcul des points
│   ├── comments.go      # GET/POST /api/matches/{id}/comments
│   └── users.go         # GET /api/leaderboard, GET /api/users/{id}, GET /api/users/me
├── sport.db             # Base de données SQLite (générée automatiquement, exclue du Git)
├── .env                 # Clé API (exclue du Git)
└── .gitignore
```

---

## Base de données

Le serveur crée automatiquement le fichier `sport.db` au premier démarrage avec 4 tables :

| Table | Description |
|---|---|
| `users` | Comptes utilisateurs (username, email, password hashé, points) |
| `matches` | Cache local des matchs récupérés depuis l'API |
| `predictions` | Pronostics soumis par les utilisateurs |
| `comments` | Commentaires sous chaque match |

---

## API REST — Endpoints

### Authentification

| Méthode | Route | Description | Auth |
|---|---|---|---|
| POST | `/api/auth/register` | Créer un compte | Non |
| POST | `/api/auth/login` | Connexion — retourne un token JWT | Non |

### Matchs

| Méthode | Route | Description | Auth |
|---|---|---|---|
| GET | `/api/matches` | Liste des matchs des 7 prochains jours | Non |
| GET | `/api/matches/{id}` | Détail d'un match | Non |

### Pronostics

| Méthode | Route | Description | Auth |
|---|---|---|---|
| POST | `/api/matches/{id}/predictions` | Soumettre un pronostic | Oui |
| GET | `/api/matches/{id}/predictions` | Voir les pronostics (après le match) | Non |

### Commentaires

| Méthode | Route | Description | Auth |
|---|---|---|---|
| GET | `/api/matches/{id}/comments` | Lire les commentaires | Non |
| POST | `/api/matches/{id}/comments` | Publier un commentaire | Oui |

### Classement & Profils

| Méthode | Route | Description | Auth |
|---|---|---|---|
| GET | `/api/leaderboard` | Classement général et hebdomadaire | Non |
| GET | `/api/users/{id}` | Profil public d'un utilisateur | Non |
| GET | `/api/users/me` | Profil de l'utilisateur connecté | Oui |

### Health

| Méthode | Route | Description |
|---|---|---|
| GET | `/api/health` | Vérifier que le serveur tourne |

---

## Authentification

Les routes protégées nécessitent un header JWT :

```
Authorization: Bearer <token>
```

Le token est obtenu via `POST /api/auth/login` et est valable **24 heures**.  
Les mots de passe sont hashés avec **bcrypt** avant stockage.  
Les requêtes SQL utilisent des paramètres préparés pour prévenir les injections SQL.

---

## Exemples d'utilisation (curl)

**Register**
```bash
curl -X POST http://localhost:8091/api/auth/register \
  -H "Content-Type: application/json" \
  -d '{"username":"dalila","email":"dalila@test.com","password":"motdepasse123"}'
```

**Login**
```bash
curl -X POST http://localhost:8091/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"dalila@test.com","password":"motdepasse123"}'
```

**Liste des matchs**
```bash
curl http://localhost:8091/api/matches
```

**Soumettre un pronostic**
```bash
curl -X POST http://localhost:8091/api/matches/6/predictions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer TON_TOKEN" \
  -d '{"home_score_pred":2,"away_score_pred":1}'
```

**Classement**
```bash
curl http://localhost:8091/api/leaderboard
```

---

## Règle de scoring

| Situation | Points |
|---|---|
| Score exact prédit (ex: 2-1 prédit, 2-1 réel) | 3 points |
| Bon vainqueur ou bon nul, score incorrect | 1 point |
| Résultat incorrect | 0 point |

---

## Scheduler automatique

Deux goroutines tournent en arrière-plan :

**Boucle nocturne (02h00 UTC)** — récupère les nouveaux matchs programmés et les résultats des matchs terminés, puis déclenche le calcul automatique des points.

**Live loop (toutes les 5 minutes)** — vérifie les matchs dont la date est passée mais dont le score n'est pas encore en base. Gère les cas de coupure réseau ou de délai de l'API.

---

## API externe — football-data.org

| Propriété | Valeur |
|---|---|
| Plan utilisé | Gratuit |
| Compétitions | FL1, PL, PD, BL1, SA, CL |
| Limite | 10 requêtes / minute |
| Données | Calendrier, résultats, statuts des matchs |

Le serveur respecte la limite de l'API en attendant 6 secondes entre chaque appel de compétition.

---

## Technologies utilisées

| Technologie | Usage |
|---|---|
| Go `net/http` | Serveur HTTP REST (sans framework) |
| SQLite (`mattn/go-sqlite3`) | Base de données locale |
| JWT (`golang-jwt/jwt`) | Authentification stateless |
| bcrypt (`golang.org/x/crypto`) | Hashage des mots de passe |
| football-data.org | API externe football |

---

## Auteurs

- **Dalila Zeghmiche** — M1 STL, Sorbonne Université
- **Feriel Benatsi** — M1 STL, Sorbonne Université

Projet réalisé dans le cadre du cours **PC3R — Programmation Concurrente, Réactive et Répartie et Réticulaire**  
Année universitaire 2025–2026

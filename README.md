# Bridge Gateway - BifrostAI

[![Go](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go)](https://go.dev/)
[![BifrostAI](https://img.shields.io/badge/BifrostAI-v1.4.4-blue)](https://github.com/maximhq/bifrost)
[![Docker](https://img.shields.io/badge/Docker-Compose-blue)](https://www.docker.com/)
[![DDD](https://img.shields.io/badge/Architecture-DDD-orange)](https://en.wikipedia.org/wiki/Domain-driven_design)

**Bridge Gateway** est une passerelle d'acces API (API Gateway) intelligente basee sur **BifrostAI**. Ecrite en **Go**, elle utilise nativement le SDK BifrostAI pour un routage performant vers differents fournisseurs LLM (Ollama par defaut, OpenAI, Azure) tout en appliquant un pipeline d'enrichissement metier (FinOps, attribution, system prompt injection).

---

## Fonctionnalites Cles

* **SDK BifrostAI Native** : Utilise `github.com/maximhq/bifrost/core` directement en Go - pas de proxy HTTP intermediaire.
* **Abstraction Multi-LLM** : Interface unique compatible avec le standard OpenAI pour requeter Ollama (defaut: `gemma4:e4b`), OpenAI, Azure, etc.
* **UI Web HTMX (Clean Architecture)** : Interface de discussion, listing et gestion des modeles, gestion des providers, projet multi-tenant avec configurations et cles API, rendue cote serveur en fragments HTMX (`hx-boost`, OOB swaps) sans JavaScript externe.
* **Enrichissement Metier** : Pipeline d'enrichissement sequentiel (5 etapes) applique a chaque requete entrante via middleware HTTP.
* **Attribution & FinOps** : Validation stricte et injection automatique du label de facturation (`cost_center`).
* **Configurations Multi-Providers** : Enregistrement de plusieurs configurations LLM (provider, modele, base URL, cle API, system prompt, cost center) persistees au format JSON.
* **Gestion des providers** : Liste des fournisseurs (nom + base URL par defaut) gerable depuis l'UI (`/providers`) — alimente le select du formulaire de configuration et l'autofill de la Base URL. Seed des defauts au premier demarrage (`data/providers.json`).
* **Catalogue de modeles** : Ajout/retrait de modeles (nom + provider) depuis l'UI (`/models`), fusionnes deduques avec `config.yaml` et les modeles utilises dans les configurations. Seed depuis `config.yaml` au premier demarrage (`data/models.json`).
* **Projets multi-tenant** : Chaque configuration appartient a un projet ; les API keys sont creees par projet et authentifient les appels `/v1/chat/completions` (la configuration active du projet sert de modele/params). Une migration auto cree le projet par defaut `General`.
* **Haute Performance** : Serveur HTTP Go natif avec gestion des timeouts et signaux de fermeture gracieuse.

---

## Architecture

```mermaid
flowchart TD
    subgraph GoApp [Bridge Gateway - Go]
        direction TB
        HTTP[HTTP Server :8080] --> MW[Enrichment Middleware]
        MW --> BifrostSDK[BifrostAI Go SDK]
    end

    BifrostSDK -->|Ollama| Ollama[(Local Ollama - gemma4:e4b)]
    BifrostSDK -->|OpenAI| OpenAI[(OpenAI API)]
    BifrostSDK -->|Azure| Azure[(Azure OpenAI)]

    classDef goStyle fill:#00ADD8,stroke:#007D9C,stroke-width:2px,color:#fff,font-weight:bold;
    classDef extStyle fill:#ECEFF1,stroke:#546E7A,stroke-width:2px;
    class HTTP,MW,BifrostSDK goStyle;
    class Ollama,OpenAI,Azure extStyle;
```

### Pipeline d'Enrichissement

Chaque requete passe par 5 etapes avant d'atteindre le LLM :

| Etape | Regle | Comportement |
|---|---|---|
| **1** | **Validation Attribution** | Verifie `cost_center` dans les metadata. Sinon: `400 Bad Request`. |
| **2** | **Injection System Prompt** | Si `system_prompt` configure et aucun role `system` present, injecte en premier message. |
| **3** | **Modele par Defaut** | Si aucun modele specifie, applique le `default_model` de la cle API. |
| **4** | **Format de Reponse** | Si non defini, applique le `response_format` configure (ex: JSON strict). |
| **5** | **Tags FinOps** | Injecte `cost_center` dans le payload pour le suivi des couts. |

---

## Structure du Projet

```
gteway-local/
├── main.go                    # Point d'entree, serveur HTTP, wiring
├── go.mod / go.sum            # Dependencies Go
├── config.yaml                # Configuration BifrostAI + modeles
├── compose.yml                # Docker Compose (passerelle + deps externes : Postgres, Redis)
├── Dockerfile                 # Build scratch (zero pull registre, binaire Linux local)
├── Makefile                   # Raccourcis build/run/test
├── data/                      # Stores JSON (configs.json, projects.json, apikeys.json)
├── domain/                    # Cœur metier (0 dependance framework)
│   ├── models.go              # Metadata, TeamMetadata, RequestMetadata
│   ├── chat.go                # ChatMessage, ChatRequest, EnrichedRequest
│   ├── config.go              # ChatConfig (entite), ChatConfigRepository (interface)
│   ├── project.go             # Project (entite) + ProjectRepository
│   ├── apikey.go              # APIKey (entite, hash sha256) + APIKeyRepository
│   ├── model.go               # ModelInfo (entite)
│   ├── llm.go                 # LLMProvider (interface)
│   ├── services.go            # EnrichmentService (pipeline d'enrichissement)
│   ├── errors.go              # MissingAttributionError, ValidationError, erreurs entites
│   └── id.go                  # Generation d'identifiants (NewID)
├── application/               # Cas d'usage (orchestration, depend des interfaces domain)
│   ├── chat.go                # ChatUseCase (envoyer un message vers le LLM)
│   ├── config.go              # ConfigUseCase (CRUD + activation, scope projet)
│   ├── project.go             # ProjectUseCase (CRUD projets)
│   ├── apikey.go              # APIKeyUseCase (creation/revocation, secret unique)
│   ├── auth.go                # AuthUseCase (master key globale ou cle projet)
│   └── model.go               # ModelUseCase (fusion modeles gateway + configurations)
├── infrastructure/            # Adaptateurs techniques
│   ├── bifrost.go             # Client BifrostAI (wrapper SDK)
│   ├── account.go             # GatewayAccount (adapteur providers)
│   ├── runtime.go             # BifrostLLMProvider (LLMProvider) + DynamicAccount par config
│   ├── jsonio.go              # Helpers de persistance JSON (thread-safe)
│   ├── config_store.go        # FileConfigStore (persistance JSON, thread-safe)
│   ├── project_store.go       # FileProjectStore
│   ├── apikey_store.go        # FileAPIKeyStore (digest uniquement)
│   └── config.go              # Chargement config.yaml
├── handlers/
│   └── chat.go                # HTTP handlers (OpenAI-compatible API + auth cle)
├── web/                       # Couche de delivery (HTMX)
│   ├── server.go              # Serveur UI (routes, templates embarquees)
│   ├── static/htmx.min.js     # HTMX vendorise (embarque dans le binaire)
│   └── templates/             # Partials + modeles (layout, chat, models, projects)
├── tests/
│   ├── services_test.go       # Tests du pipeline d'enrichissement
│   ├── application_test.go    # Tests des cas d'usage (configs CRUD, chat, modeles)
│   └── projects_test.go       # Tests projets, cles API, auth, active config par projet
└── helm/
    └── values-dev.yaml        # Valeurs Kubernetes pour dev
```

### Description des Couches

* **`domain/` (Coeur Metier)** : Independant de tout framework et de toute infrastructure. Entites (`ChatConfig`, `ModelInfo`, `ChatMessage`), interfaces de sortie (`ChatConfigRepository`, `LLMProvider`) et regles metier (pipeline d'enrichissement).
* **`application/` (Cas d'Usage)** : Orchestration des regles metier via les portes d'entree. Depend UNIQUEMENT des interfaces `domain` (inversion de dependance).
* **`infrastructure/` (Adaptateurs)** : Implantations concrètes des interfaces `domain` : client BifrostAI, store JSON file-based, comptes providers dynamiques.
* **`web/` (Delivery)** : Handlers HTTP + templates HTMX embarques (`go:embed`). Routing Go 1.22 (`GET /models`, `POST /configs/{id}`, etc.).
* **`handlers/` (API)** : API compatible OpenAI (`/v1/chat/completions`), health checks, gestion d'erreurs.

---

## Demarrage

### 1. Build et Run Local

```bash
# Compiler
make build

# Executer (UI dispo sur http://localhost:8080)
make run

# Ou directement
go run . --config config.yaml --port 8080
```

### 1b. Interface Web (HTMX)

L'application embarque une UI rendue cote serveur en HTMX (accessible a `http://localhost:8080`) :

| Route | Methode | Fonction |
|---|---|---|
| `/` | GET | Page de discussion (envoi de message vers le modele selectionne) |
| `/chat/send` | POST | Envoie le message + historique, retourne les bulles (fragment HTMX) |
| `/models` | GET | Liste des modeles (config.yaml + catalogue + configurations) |
| `/models` | POST | Ajoute un modele au catalogue |
| `/models/{id}` | DELETE | Retire un modele du catalogue |
| `/providers` | GET/POST | Liste / cree des providers |
| `/providers/{id}` | POST/DELETE | Modifie / supprime un provider |
| `/providers/{id}/edit` | GET | Formulaire d'edition (fragment HTMX) |
| `/projects` | GET/POST | Liste / cree des projets |
| `/projects/{pid}` | GET | Detail projet : configurations + API keys |
| `/projects/{pid}/configs` | POST | Cree une configuration dans le projet |
| `/projects/{pid}/configs/{cid}` | POST/DELETE | Modifie / supprime une configuration |
| `/projects/{pid}/configs/{cid}/activate` | POST | Active une configuration (exclusivite par projet) |
| `/projects/{pid}/configs/{cid}/duplicate` | POST | Duplique une configuration |
| `/projects/{pid}/configs/{cid}/edit` | GET | Formulaire d'edition (fragment HTMX) |
| `/projects/{pid}/keys` | POST | Cree une cle API (secret affiche une seule fois) |
| `/projects/{pid}/keys/{kid}` | DELETE | Revoque une cle API |
| `/configs` | GET | Redirige vers `/projects` (ancienne URL) |

Les configurations (provider, modele, base URL, cle API, system prompt, cost center, temperature, top_p, max_tokens, penalties, format de reponse) sont persistees dans `data/configs.json` et rattachees a un projet (`data/projects.json`). Les clefs API sont stockees sous forme d'empreinte SHA-256 dans `data/apikeys.json` (le secret en clair n'est jamais persistible). La navigation est propulsee par `hx-boost` (SPA-like) et les mises a jour par swaps HTMX (`innerHTML`, `outerHTML`, OOB). Le **system prompt et tous les parametres de generation de la configuration selectionnee** sont transmis au SDK Bifrost lors du chat : le system prompt est prependu comme message `system`, et les parametres partent dans les `ChatParameters` (temperature, top_p, max_tokens, penalties, response_format).

### 2. Tests

```bash
make test
```

### 3. Docker Compose (Dependances Externes uniquement)

`compose.yml` simule uniquement les **dependances externes** de la passerelle (Postgres, Redis). L'application elle-meme
est le service `gateway`. L'image est construite sans aucune dépendance à un registre (`FROM scratch`) : le binaire
Linux est cross-compilé en local (`make build-linux`) puis copié dans l'image, avec le bundle CA copie de l'hote.
Seuls les services d'infra (Postgres/Redis) necessitent un acces reseau pour leur premier pull.

> **Reseau avec Ollama** : dans le conteneur, `localhost` designe le conteneur lui-meme. Pour atteindre l'Ollama de
> votre machine hote, le service `gateway` expose `host.docker.internal` et positionne
> `OLLAMA_HOST=http://host.docker.internal:11434`. L'UI prefiltre donc automatiquement la Base URL
> `http://host.docker.internal:11434` pour le provider `ollama` en environnement conteneur (variable
> `OLLAMA_HOST`, defaut `http://localhost:11434` en lancement natif avec `make run`).

| Service   | Port publie | Role |
|---|---|---|
| **gateway**   | `4000`  | Passerelle Bridge (UI + API OpenAI) |
| **postgres**  | `5432`  | Base de donnees (dependance externe) |
| **redis**     | `6379`  | Cache (dependance externe) |

```bash
# Demarrer (build image + deps)
make docker-up

# Logs
make docker-logs

# Arreter
make docker-down

# Reinitialiser completement (volumes inclus)
make docker-reset
```

### 4. Configuration (`.env`)

```ini
# Passerelle (port publie)
BRIDGE_PORT=4000

# PostgreSQL (dependance externe)
POSTGRES_USER="bridge"
POSTGRES_PASSWORD="P6j5Zz4B4EnQZQfbkmGC"
POSTGRES_DB="bridge-db"
POSTGRES_PORT=5432

# Redis (dependance externe)
REDIS_PORT=6379

# BIFROST AI
BIFROST_MASTER_KEY="cpZ75uPavZpwRLMjD0dj"
```

---

## Exemple d'Utilisation

### Requete sur la passerelle (Port `4000`)

Deux modes d'authentification sont supportes :

* **Master key** (globale, via `BIFROST_MASTER_KEY`) — chemin historique pilote par le pipeline d'enrichissement :

```bash
curl -X POST 'http://localhost:4000/v1/chat/completions' \
-H 'Content-Type: application/json' \
-H 'Authorization: Bearer cpZ75uPavZpwRLMjD0dj' \
-d '{
    "model": "gemma4:e4b",
    "messages": [
      {"role": "user", "content": "Explique-moi le fonctionnement de BifrostAI."}
    ],
    "metadata": {
        "user_api_key_team_metadata": {
            "cost_center": "CC-INGENIERIE"
        }
    }
}'
```

* **Cle de projet** (create dans l'UI, `sk-bridge-...`) — reelle completions via la **configuration active du projet**, sans enrichissement manuel :

```bash
curl -X POST 'http://localhost:4000/v1/chat/completions' \
-H 'Content-Type: application/json' \
-H 'Authorization: Bearer sk-bridge-XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX' \
-d '{
    "model": "gemma4:e4b",
    "messages": [{"role": "user", "content": "Quel est ton nom ?"}]
}'
```

> La cle de projet doit etre copiee des sa creation : seul son prefixe (`sk-bridge-XXXXXXXX…`) et son empreinte SHA-256 sont conserves.

### Reponse

```json
{
    "id": "cmpl-bridge-gateway",
    "object": "chat.completion",
    "model": "gemma4:e4b",
    "choices": [
        {
            "index": 0,
            "message": {"role": "assistant", "content": "Enriched request processed..."},
            "finish_reason": "stop"
        }
    ]
}
```

### Health Check

```bash
curl http://localhost:8080/health/liveness
# {"status":"ok"}
```

---

## Configuration des Providers BifrostAI

Le fichier `config.yaml` definit les modeles et les parametres de routage :

```yaml
model_list:
  - model_name: gemma4:e4b
    bifrost_params:
      provider: ollama
      model: gemma4:e4b
```

Les providers sont configures dans `infrastructure/account.go` via l'interface `schemas.Account` de BifrostAI :

* **Ollama** : Modele local par defaut (`gemma4:e4b`) sur `http://localhost:11434`
* **OpenAI** : API OpenAI (GPT-4o)
* **Azure** : Azure OpenAI Service

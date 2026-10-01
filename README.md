# Bridge Gateway - BifrostAI

[![Go](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go)](https://go.dev/)
[![BifrostAI](https://img.shields.io/badge/BifrostAI-v1.4.4-blue)](https://github.com/maximhq/bifrost)
[![Docker](https://img.shields.io/badge/Docker-Compose-blue)](https://www.docker.com/)
[![DDD](https://img.shields.io/badge/Architecture-DDD-orange)](https://en.wikipedia.org/wiki/Domain-driven_design)

**Bridge Gateway** est une passerelle d'acces API (API Gateway) intelligente basee sur **BifrostAI**. Ecrite en **Go**, elle utilise nativement le SDK BifrostAI pour un routage performant vers differents fournisseurs LLM (Ollama par defaut, OpenAI, Mistral, Google/Gemini, Anthropic, Groq, Azure, Vertex, passerelles privees compatibles OpenAI) tout en appliquant un pipeline d'enrichissement metier (system prompt injection, formatage de reponse, routage par modele).

---

## Fonctionnalites Cles

* **SDK BifrostAI Native** : Utilise `github.com/maximhq/bifrost/core` directement en Go - pas de proxy HTTP intermediaire.
* **Abstraction Multi-LLM** : Interface unique compatible avec le standard OpenAI pour requeter Ollama (defaut: `gemma4:e4b`), OpenAI, Mistral, Google/Gemini, Anthropic, Groq, Azure, et n'importe quelle passerelle compatible OpenAI.
* **UI Web HTMX (Clean Architecture)** : Interface de discussion, listing et gestion des modeles, gestion des providers (avec cles d'API), projets multi-tenant avec configurations et cles API, rendue cote serveur en fragments HTMX (`hx-boost`, OOB swaps) sans JavaScript externe.
* **Enrichissement Metier** : Pipeline d'enrichissement applique a chaque requete entrante (injection de system prompt, format de reponse strict, modele par defaut).
* **Configurations Multi-Providers** : Enregistrement de plusieurs configurations LLM (provider, modele, base URL, cle API, system prompt, parametres de generation) persistees au format JSON.
* **Gestion des providers & Cles d'API** : Liste des fournisseurs (nom, base URL par defaut, cle d'API par defaut avec support des variables d'environnement ex: `{env:API_KEY}`) gerable depuis l'UI (`/providers`). Autofill et fallback automatique de la cle API et de l'URL lors des configurations. Seed des defauts au premier demarrage (`data/providers.json`).
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
    BifrostSDK -->|OpenAI / Compatible| OpenAI[(OpenAI / Passerelles Privees)]
    BifrostSDK -->|Mistral| Mistral[(Mistral API)]
    BifrostSDK -->|Google / Gemini| Google[(Google Gemini)]
    BifrostSDK -->|Azure| Azure[(Azure OpenAI)]

    classDef goStyle fill:#00ADD8,stroke:#007D9C,stroke-width:2px,color:#fff,font-weight:bold;
    classDef extStyle fill:#ECEFF1,stroke:#546E7A,stroke-width:2px;
    class HTTP,MW,BifrostSDK goStyle;
    class Ollama,OpenAI,Mistral,Google,Azure extStyle;
```

### Pipeline d'Enrichissement

Chaque requete passe par les etapes suivantes avant d'atteindre le LLM :

| Etape | Regle | Comportement |
|---|---|---|
| **1** | **Injection System Prompt** | Si `system_prompt` configure et aucun role `system` present, injecte en premier message. |
| **2** | **Modele par Defaut** | Si aucun modele specifie, applique le `default_model` configure. |
| **3** | **Format de Reponse** | Si non defini, applique le `response_format` configure (ex: JSON strict). |

---

## Structure du Projet

```
gteway-local/
├── main.go                    # Point d'entree, serveur HTTP, wiring
├── go.mod / go.sum            # Dependencies Go
├── config.yaml                # Configuration BifrostAI + modeles
├── compose.yml                # Docker Compose (passerelle autonome : SQLite + cache memoire)
├── Dockerfile                 # Build scratch (zero pull registre, binaire Linux local)
├── Makefile                   # Raccourcis build/run/test
├── data/                      # Base SQLite (bridge.db) + anciens stores JSON a migrer
├── domain/                    # Cœur metier (0 dependance framework)
│   ├── models.go              # Metadata, TeamMetadata, RequestMetadata
│   ├── chat.go                # ChatMessage, ChatRequest, EnrichedRequest
│   ├── config.go              # ChatConfig (entite), ChatConfigRepository (interface)
│   ├── project.go             # Project (entite) + ProjectRepository
│   ├── apikey.go              # APIKey (entite, hash sha256) + APIKeyRepository
│   ├── provider.go            # Provider (entite, baseURL + apiKey par defaut)
│   ├── model.go               # ModelInfo (entite)
│   ├── llm.go                 # LLMProvider (interface)
│   ├── services.go            # EnrichmentService (pipeline d'enrichissement)
│   ├── errors.go              # ValidationError, erreurs entites
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

Les configurations (provider, modele, base URL, cle API, system prompt, temperature, top_p, max_tokens, penalties, format de reponse) sont persistees dans `data/configs.json` et rattachees a un projet (`data/projects.json`). Les clefs API sont stockees sous forme d'empreinte SHA-256 dans `data/apikeys.json` (le secret en clair n'est jamais persistible). La navigation est propulsee par `hx-boost` (SPA-like) et les mises a jour par swaps HTMX (`innerHTML`, `outerHTML`, OOB). Le **system prompt et tous les parametres de generation de la configuration selectionnee** sont transmis au SDK Bifrost lors du chat : le system prompt est prependu comme message `system`, et les parametres partent dans les `ChatParameters` (temperature, top_p, max_tokens, penalties, response_format).

### 2. Tests

```bash
make test
```

### 3. Docker Compose (Passerelle Autonome)

La passerelle n'a **aucune dependance externe** : la persistance est un fichier SQLite (`data/bridge.db`) et le cache
est local au processus (en memoire). Postgres et Redis ont ete supprimes.

`compose.yml` ne declare donc qu'un service, `gateway`. L'image est construite sans aucune dependance a un registre
(`FROM scratch`) : le binaire Linux est cross-compile en local (`make build-linux`) puis copie dans l'image, avec le
bundle CA copie de l'hote. Le driver SQLite utilise est **pur Go** (`modernc.org/sqlite`), donc `CGO_ENABLED=0` reste
compatible avec ce build.

> **Reseau avec Ollama** : dans le conteneur, `localhost` designe le conteneur lui-meme. Pour atteindre l'Ollama de
> votre machine hote, le service `gateway` expose `host.docker.internal` et positionne
> `OLLAMA_HOST=http://host.docker.internal:11434`. L'UI prefiltre donc automatiquement la Base URL
> `http://host.docker.internal:11434` pour le provider `ollama` en environnement conteneur (variable
> `OLLAMA_HOST`, defaut `http://localhost:11434` en lancement natif avec `make run`).

| Service   | Port publie | Role |
|---|---|---|
| **gateway**   | `4000`  | Passerelle Bridge (UI + API OpenAI) |

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

# Persistance : SQLite, un simple fichier monte dans ./data
# BRIDGE_DB="/app/data/bridge.db"

# Cache : local au processus, regle dans config.yaml (section `cache`)

# BIFROST AI
BIFROST_MASTER_KEY="cpZ75uPavZpwRLMjD0dj"
```

### 5. Persistance et cache

**SQLite.** Toute la persistance passe par un fichier unique (`data/bridge.db` par defaut, surchargeable via
`--db`). Le schema est cree au demarrage (`Migrate`) et l'import des anciens fichiers JSON de `data/` est automatique
et idempotent au premier lancement : chaque fichier migre est renomme en `.imported`, et relancer l'import ne
duplique rien. Pour ignorer l'import, passer `--import-json ""`.

> Un projet existant garde ses donnees : l'import est execute **avant** le seed, donc les providers et modeles deja
> presents dans les JSON sont conserves (les 8 providers par defaut ne sont ajoutes que sur une base vide).

**Cache en memoire.** Deux caches independants, tous deux configurables dans la section `cache` de `config.yaml` et
desactivables (`enabled: false`) sans toucher au code :

| Cache | TTL par defaut | Contenu |
|---|---|---|
| `cache.stores` | `15s` | Lectures de repositories : cles API par digest, config active, projets |
| `cache.llm_responses` | `5m` | Reponses LLM identiques (evite un appel reseau au provider) |

Les entrees expirent (TTL) et les plus anciennes sont evincees au-delà de `max_entries`. Les ecritures invalident les
cles concernees, et les erreurs provider ne sont **jamais** mises en cache.

---

## Exemple d'Utilisation

### Requete sur la passerelle (Port `4000`)

Deux modes d'authentification sont supportes :

* **Master key** (globale, via `BIFROST_MASTER_KEY`) — requete directe avec routage et enrichissement standard :

```bash
curl -X POST 'http://localhost:4000/v1/chat/completions' \
-H 'Content-Type: application/json' \
-H 'Authorization: Bearer cpZ75uPavZpwRLMjD0dj' \
-d '{
    "model": "gemma4:e4b",
    "messages": [
      {"role": "user", "content": "Explique-moi le fonctionnement de BifrostAI."}
    ]
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

curl -X POST 'http://localhost:4000/v1/chat/completions' \
-H 'Content-Type: application/json' \
-H 'Authorization: Bearer sk-bridge-5a09f2a7b972bd4da3705a18c10b8c5d5166514c2225f3935c3a3c22e9fcb6e9' \
-d '{
    "model": "gemma4:e4b",
    "messages": [{"role": "user", "content": "Quel est ton nom ?"}]
}

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

Le fichier `config.yaml` definit les modeles et les parametres de routage par defaut :

```yaml
model_list:
  - model_name: gemma4:e4b
    bifrost_params:
      provider: ollama
      model: gemma4:e4b
```

Les providers peuvent etre configures globalement dans `config.yaml` ou dynamiquement via l'interface `/providers` et `/projects` :

* **Ollama** : Modele local par defaut (`gemma4:e4b`) sur `http://localhost:11434`
* **OpenAI / Mistral / Anthropic / Groq / Google Gemini / Azure / Vertex**
* **Passerelles compatibles OpenAI** : N'importe quelle API / proxy LLM respectant le protocole OpenAI avec une `baseURL` et `apiKey` personnalisees (support des variables d'environnement ex: `{env:SECRET_KEY}`).

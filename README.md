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
* **Moteurs locaux ONNX** : embeddings **et chat** executes in-process (runtime charge en pure Go, sans CGO ni service compagnon). Artefacts sous `models/onnx/` ignores par git mais **telecharges au premier lancement** (`ONNX_AUTO_DOWNLOAD=0` pour desactiver), modeles discovers au demarrage et semes au catalogue.
* **Projets multi-tenant** : Chaque configuration appartient a un projet ; les API keys sont creees par projet et authentifient les appels `/v1/chat/completions` (la configuration active du projet sert de modele/params). Une migration auto cree le projet par defaut `General`.
* **Haute Performance** : Serveur HTTP Go natif avec gestion des timeouts et signaux de fermeture gracieuse.

---

## Architecture

```mermaid
flowchart TD
    subgraph GoApp [Bridge Gateway - Go]
        direction TB
        HTTP[HTTP Server :BRIDGE_PORT] --> MW[Enrichment Middleware]
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
* **`handlers/` (API)** : API compatible OpenAI (`/v1/chat/completions` y compris en SSE, `/v1/models`), health checks, gestion d'erreurs. Le routage `/v1` est centralise dans `handlers.RegisterRoutes`, partage entre `main.go` et les tests de contrat. Le streaming est dans `handlers/stream.go`, la reponse unique dans `chat.go`.

---

## Demarrage

### 1. Build et Run Local

```bash
# Compiler
make build

# Executer (port : BRIDGE_PORT du .env, defaut 4000 — UI sur http://localhost:4000)
make run

# Ou directement (idem ; "--port" force une valeur differente)
go run . --config config.yaml
```

### 1b. Interface Web (HTMX)

L'application embarque une UI rendue cote serveur en HTMX (accessible a `http://localhost:4000`, port `BRIDGE_PORT` du `.env`) :

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

`BRIDGE_PORT` determine le port publie (docker compose) **et** le port d'ecoute
local : la passerelle lit `.env` au demarrage, sans export prealable. Un
`--port` explicite reste prioritaire — c'est le cas dans le conteneur, qui
ecoute en interne sur 8080 pendant que l'hote publie `BRIDGE_PORT`.

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

### 6. Routage par tier et bascule sur saturation

Desactive par defaut (`routing.enabled: false`). Quand il est actif, chaque requete part vers le modele le moins
couteux qui suffise, et remonte d'un cran si le fournisseur ne peut pas servir.

**Declencher le routage.** Attribuer un *tier* a chaque configuration du projet depuis l'onglet Projects
(`fast` < `balanced` < `frontier`). Une configuration sans tier reste utilisable et sert de point de depart, mais ne
figure pas dans l'echelle. Le champ est stocke en JSON dans `chat_configs` : aucun `ALTER TABLE` n'est necessaire.

**Deux mecanismes, la meme direction.**

- Le **classifieur** (un petit modele local) evalue la complexite de la conversation *entiere*, pas seulement le dernier
  message : une conversation qui s'allonge exige naturellement plus de puissance. Il ne peut ni descendre sous le tier
  choisi explicitement par l'utilisateur, ni envoyer une requete complexe vers un modele trop faible.
- La **saturation** fait monter d'un cran quand le provider echoue :

  | Declencheur | Exemple | Remede |
  |---|---|---|
  | `rate_limit` | HTTP 429, quota epuise | un autre provider a son propre quota |
  | `model_unavailable` | HTTP 404 sur le nom du modele | un autre tier peut le servir |
  | `context_length` | HTTP 400, historique trop long | un modele plus grand |
  | `unavailable` | HTTP 5xx, timeout, reseau | panne isolee a un provider |
  | `auth` | HTTP 401 / 403 | **peut masquer une mauvaise cle API** — reste visible dans les logs |

  Un 4xx attribuable a la requete elle-meme (JSON invalide, mauvais chemin d'API) **n'entraine aucune bascule** : aucun
  autre modele ne la rendrait valide, et multiplier les appels ne ferait qu'aggraver la situation.

**Ce qui ne bascule pas.** Seuls `provider`, `model`, `base_url` et `api_key` proviennent du tier cible. Le system
prompt, la temperature, le `top_p`, les penalties et le format de reponse restent ceux de la configuration active :
changer de modele ne doit pas changer silencieusement le comportement d'une conversation en cours.

**Cooldown.** Apres un echec, le tier reste ecarte pendant `routing.cooldown` (`60s` par defaut), ce qui evite de
rebruler un modele mort sur chaque requete. La cle est `provider/model` : corriger la configuration vers un autre
modele la rend de nouveau eligible immediatement. Un cooldown qui viderait toute l'echelle est ignore, pour ne pas
preferer ne pas repondre.

**Configuration.**

```yaml
routing:
  enabled: true
  min_confidence: 0.6    # sous ce seuil, la config active est conservee
  cooldown: "60s"        # 0 desactive la memorisation des saturations
  max_attempts: 0        # 0 = toute l'echelle
  max_retries: 1         # reessais Bifrost par appel (2 sinon)
  classifier:
    provider: ollama
    model: granite4:tiny-h   # voir l'avertissement de latence ci-dessous
    max_input_chars: 6000
    max_tokens: 64
    timeout: "10s"           # au-dela, la precision est sacrifiee, pas la requete
```

**Latence : le point d'attention.** Le classifieur est un appel LLM supplementaire, sur **chaque requete qui n'est
pas un hit de cache**. Mesure sur cette machine, `gemma4:e4b` comme classifieur a coute **~25 s** par requete, ce qui
est inacceptable. Deux leviers :

1. mettre un **vraiment petit modele** en `classifier.model` (`granite4:tiny-h`, `qwen2.5`, `smollm2`) — c'est le
   levier principal ;
2. fixer `classifier.timeout` : au-dela, le classifieur est abandonne et la configuration active est conservee. La
   reponse est alors servie au prix de la precision du routage, jamais au prix de la perte de la requete.

Le cache de reponses absorbe les repetitions exactes : la cle est calculee sur la configuration active, donc un hit
evite a la fois la classification et toute l'echelle de bascule.

**Observabilite.** Les decisions sont journalisees, et la reponse HTTP renvoie le modele qui a **reellement** produit
le texte (champ `model`), pas celui de la configuration demandee. L'UI affiche « Reponse produite par ... » des que les
deux different.

---

### 1c. Modeles d'embedding locaux

Le testeur d'embeddings (`/embeddings-test`) et l'API `/v1/embeddings` choisissent leur moteur d'apres le champ
`model` de la requete (jamais par la configuration du projet). Trois modeles sont curates d'entree :

| `model` | Moteur | Dimensions | Pre-requis |
|---|---|---|---|
| `local-embedding` | Vectoriseur Go embarque dans le binaire | 384 | aucun |
| `nomic-embed-text` | Ollama (`localhost:11434`) via Bifrost | 768 | `ollama pull nomic-embed-text` |
| `onnx/all-MiniLM-L6-v2` | Runtime ONNX in-process (purego, sans CGO) | 384 | artefacts sous `models/onnx/` (ci-dessous) |

`models/onnx/` est ignore par git : **au premier lancement, les artefacts manquants sont telecharges
automatiquement** (bibliotheque ONNX pour la plateforme, modeles d'embedding, modele de chat — environ 190 Mo,
logues `[onnx] ...` puis en place de facon atomique). Un echec reseau n'empeche jamais le serveur de demarrer :
il est journalise, le prochain demarrage reprend, et chaque choix ONNX reste une erreur explicite citant le fichier
manquant. Desactiver avec `ONNX_AUTO_DOWNLOAD=0` (artefacts fournis a la main ou environnement hors-ligne).

En pilotage manuel (meme sources que celles du manifeste), adapter l'archive ONNX Runtime a la plateforme
(`osx-arm64`, `linux-x64`, `win-x64` ; la bibliotheque doit etre en **1.23.x**, l'API C attendue est la version 23) :

```bash
mkdir -p models/onnx/lib
# libonnxruntime.{dylib,so,dll} depuis les releases microsoft/onnxruntime (v1.23.0), placee dans models/onnx/lib/
curl -L -o models/onnx/model.onnx https://huggingface.co/Xenova/all-MiniLM-L6-v2/resolve/main/onnx/model_quantized.onnx
curl -L -o models/onnx/vocab.txt  https://huggingface.co/Xenova/all-MiniLM-L6-v2/resolve/main/vocab.txt
```

Chemins surchargeables : `ONNX_RUNTIME_LIB`, `ONNX_MODEL_PATH`, `ONNX_VOCAB_PATH`, `ONNX_MAX_SEQ_LEN` (defaut 128).
Sans artefacts, le choix ONNX rend une erreur explicite citant le fichier manquant (400 sur l'API, toast sur le
testeur) — jamais de repli silencieux vers un provider distant. `go test ./...` saute le test d'integration ONNX
tant que les artefacts sont absents.

**Ajouter des modeles depuis l'UI.** La page `Modeles` propose un champ **Type** (`chat` par defaut,
`embedding`) : un modele declare `embedding` rejoint le select du testeur et l'API `/v1/embeddings` — le routeur
le resout vers son moteur d'apres le provider enregistre (recommande : `local`, `onnx` ou `ollama`). Un modele de
chat, meme homonyme, ne detourne jamais un appel d'embedding.

**Plusieurs modeles ONNX locaux.** Chaque repertoire de `models/onnx/` contenant `model.onnx` + `vocab.txt`
devient un modele selectable sous l'id `onnx/<repertoire>` : decouvert automatiquement au demarrage, seme au
catalogue (type `embedding`), donc visible sur la page `Modeles`, dans le testeur et depuis l'API :

```bash
mkdir -p models/onnx/paraphrase-MiniLM-L3-v2
# vocab.txt : memes vocabulaires BERT pour les MiniLM, copie du modele par defaut si absent
curl -L -o models/onnx/paraphrase-MiniLM-L3-v2/model.onnx https://huggingface.co/Xenova/paraphrase-MiniLM-L3-v2/resolve/main/onnx/model_quantized.onnx
```

Le modele par defaut (`onnx/all-MiniLM-L6-v2`) reste sur les chemins plats ci-dessus (variables
`ONNX_MODEL_PATH` / `ONNX_VOCAB_PATH`) ; la convention par repertoire ne vaut que pour les autres modeles. Un
identifiant inconnu rend une erreur citant le repertoire attendu, et chaque modele charge a sa session en memoire
(paresseusement, a son premier appel).

---

### 1d. Modeles de chat ONNX locaux

Un repertoire `models/onnx/chat/<id>/` contenant `model.onnx` + `tokenizer.json` + `config.json` devient un
**mode de chat executable localement**, sous le provider `onnx` :

* **Decouverte et catalogue** : les modeles trouves sont semes au catalogue (type `chat`, provider `onnx`) et
  proposés sur la page `Modeles` ; creer une configuration avec provider `onnx` et ce modele suffit a router la
  conversation localement (le routeur choisit le moteur avant Bifrost, sans bascule silencieuse).
* **Moteur** : prefill puis decode avec cache KV, echantillonnage temperature / top_p (argmax si temperature a 0),
  arret sur le `eos_token_id` de `generation_config.json` ; tokenizer BPE pur Go lu dans `tokenizer.json` (les deux
  serialisations de merges sont acceptees). Streaming SSE natif, fragment par fragment.
* **Template de chat** : applique automatiquement (system par defaut, cadre `role`), les identifiants de la
  configuration active restent ignores — le modele est executé in-process, sans cle ni URL.
* **Premier lancement** : l'ensemble (modele + runtime) fait partie du manifeste de telechargement automatique
  de la section 1c (~190 Mo au total).

Le modele de reference est `smollm2-135m-instruct` (135 M de parametres, ~131 Mo quantise). Fenetre et budget
reglables par `ONNX_CHAT_MAX_SEQ_LEN` (defaut 2048) et `ONNX_CHAT_MAX_NEW_TOKENS` (defaut 256).

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
    "id": "chatcmpl-83d6a3332f460d947812b535",
    "object": "chat.completion",
    "created": 1791195410,
    "model": "gemma4:e4b",
    "choices": [
        {
            "index": 0,
            "message": {"role": "assistant", "content": "Enriched request processed..."},
            "finish_reason": "stop"
        }
    ],
    "usage": {"prompt_tokens": 0, "completion_tokens": 0, "total_tokens": 0}
}
```

### Routes de l'API standard OpenAI

| Methode | Chemin | Role |
| --- | --- | --- |
| `POST` | `/v1/chat/completions` | Completion de chat, SSE si `stream: true` (auth `Bearer`) |
| `POST` | `/v1/embeddings` | Vecteurs d'embedding (auth `Bearer`) |
| `GET` | `/v1/models` | Liste des modeles connus |
| `GET` | `/v1/models/{model}` | Detail d'un modele, insensible a la casse |
| `GET` | `/health/liveness`, `/health/readiness`, `/health/test_connection` | Sondes JSON sans authentification |

Toute route `/v1/...` exige une cle. Toute autre route `/v1/` repond `404` avec
l'enveloppe d'erreur OpenAI, et non le texte brut de `net/http` :

```bash
curl http://localhost:4000/v1/models -H 'Authorization: Bearer sk-bridge-XXXX'
# {"object":"list","data":[{"id":"gemma4:e4b","object":"model","created":1791195330,"owned_by":"ollama"}]}

curl -X POST http://localhost:4000/v1/embeddings -H 'Authorization: Bearer sk-bridge-XXXX' \
  -H 'Content-Type: application/json' -d '{"input":"bonjour","model":"onnx/all-MiniLM-L6-v2"}'
# {"object":"list","data":[{"object":"embedding","index":0,"embedding":[...]}],"model":"onnx/all-MiniLM-L6-v2",...}

curl -X POST http://localhost:4000/v1/embeddings -d '{}'
# {"error":{"code":"invalid_api_key","message":"invalid or missing API key","param":null,"type":"authentication_error"}}
```

Les erreurs suivent le paquet `error` d'OpenAI (`message`, `type`, `param`, `code`).

### Documentation API (Swagger)

La specification OpenAPI 3 est **embarquee dans le binaire** et servie en local :

| Chemin | Contenu |
| --- | --- |
| `GET /swagger` | Interface Swagger UI ("Try it out" inclus) |
| `GET /openapi.yaml` | Specification OpenAPI 3 (`application/yaml`) |

```bash
curl http://localhost:4000/openapi.yaml | head -3
# openapi: 3.0.3
# info:
#   title: Bridge Gateway API
```

Les deux routes sont sans authentification : la documentation decrit l'API, elle ne
l'expose pas — chaque route `/v1/` reste protegee par sa propre cle. L'UI Swagger
charge ses assets depuis un CDN (`unpkg`) ; la specification, elle, est servie en
local et reste utilisable hors-ligne (Redoc, generateur de SDK : `npx @openapitools/openapi-generator-cli generate -i http://localhost:4000/openapi.yaml -g go`).

Les requetes "Try it out" rappellent l'origine qui sert la page : la specification
declare `:4000` (BRIDGE_PORT du `.env`, docker compose et `make run`) et `:8080`
(repli sans `.env`), dont une seule — parfois aucune — repond selon le mode de
demarrage. Parallelement, les routes `/v1/`,
`/health/` et la documentation exposent les en-tetes CORS (preflight `OPTIONS`
inclus) pour tout appel cross-origin de navigateur ; les routes de l'UI web,
sans authentification, restent hors CORS.

### Streaming

`"stream": true` bascule la meme route en SSE (`text/event-stream`), sans
`/v1/chat/completions/stream` ni autre chemin : c'est le comportement attendu par
les SDK.

```bash
curl -N -X POST 'http://localhost:4000/v1/chat/completions' \
-H 'Content-Type: application/json' \
-H 'Authorization: Bearer sk-bridge-XXXX' \
-d '{"model":"gemma4:e4b","stream":true,"messages":[{"role":"user","content":"Raconte une histoire."}]}'
```

La sequence des trames est celle d'OpenAI : une trame portant le role
`assistant`, une trame par increment de texte, une trame de fin avec
`finish_reason: "stop"`, puis `data: [DONE]`. Toutes les trames partagent le meme
`id` et le meme `created`.

`stream_options.include_usage` ajoute, juste avant `[DONE]`, une trame de
consommation avec `choices` vide, comme chez OpenAI :

```json
{"id":"chatcmpl-…","object":"chat.completion.chunk","created":1791196744,
 "model":"gemma4:e4b","choices":[],
 "usage":{"prompt_tokens":10,"completion_tokens":10,"total_tokens":20}}
```

Le streaming traverse toute la chaine (cache, routeur, Bifrost) :

* **cache** : un hit est redonne en un seul fragment, un miss est streamed puis
  memorise. Une reponse interrompue n'est jamais memorisee ;
* **routeur** : la bascule vers un tier superieur n'est possible que tant que le
  client n'a recu aucun fragment. Au-dela, une coupure lui est annoncee dans une
  trame d'erreur, car le texte est deja chez lui ;
* **provider non streame** : la reponse est redonnee en un seul fragment, donc
  toujours en SSE valide.

Verifier avec le SDK officiel :

```python
from openai import OpenAI
client = OpenAI(base_url="http://localhost:4000/v1", api_key="sk-bridge-XXXX")
for chunk in client.chat.completions.create(model="gemma4:e4b", messages=[...], stream=True):
    print(chunk.choices[0].delta.content or "", end="", flush=True)
```

### Sortie structuree (JSON mode et schemas)

Le format de reponse se configure dans la configuration du projet, et une
requete peut le surcharger. **La requete l'emporte toujours**, comme chez OpenAI.

Trois formats sont acceptes, et `json_schema` exige un schema :

```bash
# schema transmis par la requete
curl -X POST 'http://localhost:4000/v1/chat/completions' \
-H 'Authorization: Bearer sk-bridge-XXXX' \
-H 'Content-Type: application/json' \
-d '{
  "messages": [{"role":"user","content":"Ville et population"}],
  "response_format": {
    "type": "json_schema",
    "json_schema": {
      "name": "ville",
      "schema": {
        "type": "object",
        "properties": {"ville": {"type":"string"}, "population": {"type":"integer"}},
        "required": ["ville","population"],
        "additionalProperties": false
      }
    }
  }
}'

# JSON valide, sans schema : {"type":"json_object"}
-d '{"messages":[...],"response_format":{"type":"json_object"}}'
```

La surcharge vaut aussi pour `temperature`, `top_p`, `max_tokens`,
`max_completion_tokens`, `frequency_penalty` et `presence_penalty`. Elle est
appliquee sur une copie de la configuration : la configuration enregistree n'est
jamais modifiee par un appel, et le modele n'est jamais surcharge.

Un `response_format` invalide (type inconnu, schema manquant ou schema qui n'est
pas un objet) est refuse en `400` avec `param: "response_format"`.

**Constructeur visuel.** Le formulaire de configuration expose un constructeur de
schema : on declare les champs, et le serveur produit le JSON Schema qui sera
envoye au provider.

Le tableau accepte **autant de lignes que necessaire** — le bouton « Ajouter un
champ » en clone une ligne du `<template>`, chaque ligne a son bouton de
suppression, et la numerotation est recalculee apres chaque ajout ou retrait
(appariement de la case « obligatoire » par position de ligne). La logique vit
dans le layout, en delegation sur `document`, donc elle survit aux swaps htmx :
le `<template>` est reconstruit a chaque rechargement de la section, et les
lignes deja saisies sont relues a l'edition.

Colonnes disponibles par champ :

| Colonne        | Effet sur le schema                          |
| -------------- | -------------------------------------------- |
| Nom            | nom de la propriete                          |
| Type           | `string`, `number`, `integer`, `boolean`, `object`, `array` |
| Description    | `description`                                |
| Obligatoire    | entre dans `required`                        |
| Valeurs        | `enum` (liste separee par des virgules)      |
| Min / Max      | `minLength`/`maxLength` (chaine) ou `minimum`/`maximum` (nombre), selon le type |
| Motif          | `pattern` (chaine)                           |
| Type d'element | `items.type` (tableau ; `string` par defaut) |

Les contraintes ne sont ajoutees que si elles sont saisies, et une contrainte qui
ne correspond pas au type du champ est ignoreee plutot que rejetee : changer le
type d'un champ ne doit pas faire echouer la sauvegarde. Un schema dont la
construction est invalide n'est pas enregistre, la validation de la
configuration signalant alors que `json_schema` exige un schema.

La textarea « Schema JSON » permet de saisir directement un schema que le
constructeur n'exprime pas (imbrication, unions, `oneOf`) ; **elle est
prioritaire** quand elle est remplie.

Les deux chemins convergent vers le meme schema : construit depuis le formulaire,
stocke dans la configuration, ou fourni directement dans le `response_format`
d'une requete API.

> **Un schema enregistre vaut declaration d'intention.** Si la configuration
> porte un `response_schema` mais que le select « Format de reponse » est laisse
> sur « aucun », le format effectif est **deduit** comme `json_schema` : le
> schema est alors reellement transmis au provider. Le formulaire affiche
> egalement la valeur effective a l'edition, pour ne pas laisser croire que le
> schema est ignore. Un format choisi explicitement (`text`, `json_object`)
> reste prioritaire — la deduction ne s'applique qu'en l'absence de format.

> **Mode strict.** OpenAI impose qu'en mode `strict` *tous* les champs declares
> soient requis. Le champ `strict` n'est donc annonce que si c'est vrai du schema
> : un schema comportant un champ facultatif est envoye avec `strict: false`,
> plutot que d'etre refuse par le provider.

### Health Check

```bash
curl http://localhost:4000/health/liveness
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

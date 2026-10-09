# TODO

Suivi des travaux d'architecture (Clean Architecture / SOLID) de Bridge Gateway.

## Fait

- [x] **Analyse des dépendances entre couches** — `domain` (0 dépendance projet),
  `application` et `infrastructure` → `domain` uniquement, `handlers`/`web` →
  `application` + `domain`, composition root isolée dans `main.go`.
- [x] **DIP + ISP dans la couche delivery** — interfaces de consommateur déclarées
  côté consommateur, avec exactement les méthodes utilisées :
  - `handlers/ports.go` : `Authenticator`, `Enricher`, `ChatCompletion`,
    `ActiveConfigLoader`, `ConfigQuerier`, `ModelLister`, `Embedder` ;
  - `web/ports.go` : `chatService`, `configService`, `modelService`,
    `projectService`, `keyService`, `providerService`, `catalogService`,
    `embeddingService` ;
  - les champs et constructeurs de `handlers/*` et `web/server.go` ne référencent
    plus aucun type concret de use case.
- [x] **SRP / code mort** — suppression de
  `EnrichmentService.ValidateAttribution` (jamais appelé, retournait `nil`).
- [x] **`main.go` sur les ports du domaine** — `migrateOrphanConfigs` accepte
  `domain.ChatConfigRepository` / `domain.ProjectRepository` au lieu des classes
  SQLite ; instance unique de `ChatUseCase` partagée API + UI.
- [x] **README** — arborescence recalée sur le dépôt réel, section
  « Regles de dependance (Clean Architecture / SOLID) » ajoutée.
- [x] **Vérification** — `go build ./...`, `go vet ./...`, `go test ./...` au vert
  (aucun test sauté ni affaibli) ; dépendances entre couches re-vérifiées.
- [x] **SRP sur `web/server.go`** — le type « god object » (42 méthodes, ~1200
  lignes) a été découpé en sous-contrôleurs par domaine partageant `uiShared`
  (rendu + helpers) : `chat.go`, `projects.go`, `models.go`, `providers.go`,
  `embeddings.go`, `forms.go` (parties pures) ; `server.go` n'est plus qu'une
  façade (types, wiring, routing). Corps de fonctions vérifiés identiques à
  l'original, 26 routes inchangées, suite de tests au vert.

## A faire

- [ ] **`web.NewServer` ne devrait pas tuer le process** — remplacer le
  `log.Fatalf` sur l'échec de parsing des templates par un retour
  `(*Server, error)` ; 6 sites d'appel à mettre à jour (`main.go` + 5 tests `web`).
- [ ] **Monter la politique de routage en `domain/`** — le choix de tier et la
  classification des échecs (`failoverReason`) vivent dans
  `infrastructure/routing.go` ; défendable comme décorateur du port
  `LLMProvider`, mais déplaçable en `domain/` si on veut une règle métier 100 %
  framework-free.
- [ ] **Tests des ports delivery** — les interfaces de consommateur rendent
  désormais possibles des stubs légers pour `handlers`/`web` (au lieu
  d'instancier toute la chaîne SQLite) ; à exploiter pour de nouveaux tests
  unitaires de la couche presentation.
- [ ] **Vérifier la couverture de `application/`** — paquet actuellement sans
  fichier de test dédié (exercé via `tests/application_test.go`) ; un
  `application/*_test.go` au plus près du code clarifierait les régressions.

## Idées écartées (pour l'instant)

- Fusionner `handlers` et `web` en une seule couche delivery : les deux surfaces
  (API OpenAI vs UI HTMX) ont des contrats différents ; garder la séparation.
- Introduire un framework d'injection de dépendances : le wiring manuel dans
  `main.go` (composition root) est explicite et suffit à cette échelle.

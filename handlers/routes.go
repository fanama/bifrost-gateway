package handlers

import "net/http"

// RegisterRoutes enregistre la surface API standard OpenAI sur mux.
//
// Elle est volontairement isolee de main() pour que les tests de contrat
// exercent exactement les memes enregistrements que la production : une
// divergence entre les deux ferait passer des tests sur des routes qui ne
// sont pas celles qui ecouvrent.
//
// Les routes sont declarees sans methode, le controle revenant aux handlers :
// c'est ce qui permet de repondre 405 plutot que le 404 du repli "/v1/".
func RegisterRoutes(mux *http.ServeMux, chat *ChatHandler, models *ModelsHandler) {
	// Generation de texte.
	mux.HandleFunc("/v1/chat/completions", chat.HandleChatCompletion)

	// Decouverte de modeles : les SDK et les agents interrogent /v1/models
	// avant de choisir un modele.
	mux.HandleFunc("/v1/models", models.HandleList)
	mux.HandleFunc("/v1/models/{model}", models.HandleRetrieve)

	// Repli pour toute route /v1/ inconnue : enveloppe d'erreur JSON plutot
	// que le 404 en texte brut de net/http. "/v1/" reste moins specifique que
	// les routes ci-dessus, donc il ne les masque pas.
	mux.HandleFunc("/v1/", models.HandleNotFound)
}

package infrastructure

import (
	"strconv"
	"strings"
)

// DefaultPort est le port d'ecoute quand aucune source n'en fournit un :
// ni le drapeau --port, ni BRIDGE_PORT de l'environnement, ni BRIDGE_PORT
// du fichier .env.
const DefaultPort = 8080

// dotenvFile est lu depuis le repertoire de travail, comme config.yaml.
const dotenvFile = ".env"

// ResolvePort choisit le port d'ecoute de la passerelle, dans l'ordre :
//
//  1. le drapeau --port, quand il est explicitement passe — c'est ce qui
//     preserve le conteneur, dont la CMD porte "--port 8080" alors que le
//     meme .env expose BRIDGE_PORT=4000 cote hotte ;
//  2. la variable d'environnement BRIDGE_PORT ;
//  3. BRIDGE_PORT du fichier .env, pour un demarrage local qui n'a pas
//     exporte la variable — c'est la source attendue par defaut ;
//  4. DefaultPort (8080), en dernier repli.
//
// Une valeur hors [1, 65535] est ignoree et la source suivante est
// essayee : un BRIDGE_PORT malforme ne doit pas empecher le demarrage.
//
// lookupEnv et readFile sont injectes pour que les tests n'aient ni a
// toucher a l'environnement du processus, ni a ecrire de fichier .env.
func ResolvePort(flagPort int, flagSet bool, lookupEnv func(string) (string, bool), readFile func(string) ([]byte, error)) int {
	if flagSet {
		return flagPort
	}
	if raw, ok := lookupEnv("BRIDGE_PORT"); ok {
		if port, valid := PortFromValue(raw); valid {
			return port
		}
	}
	if data, err := readFile(dotenvFile); err == nil {
		if port, valid := PortFromDotEnv(data); valid {
			return port
		}
	}
	return DefaultPort
}

// PortFromValue interprete une chaine en port : espaces toleres, entier
// strictement positif dans la plage valide.
func PortFromValue(raw string) (int, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	port, err := strconv.Atoi(raw)
	if err != nil || port < 1 || port > 65535 {
		return 0, false
	}
	return port, true
}

// PortFromDotEnv relit BRIDGE_PORT d'un fichier .env : lignes vides et
// commentaires ignores, "export " et guillemets facultatifs, espaces autour
// de "=" toleres. La derniere affectation valide gagne, comme le ferait un
// chargeur d'environnement.
func PortFromDotEnv(data []byte) (int, bool) {
	best, found := 0, false
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, value, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(key) != "BRIDGE_PORT" {
			continue
		}
		value = strings.TrimSpace(value)
		if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0] {
			value = value[1 : len(value)-1]
		}
		if port, valid := PortFromValue(value); valid {
			best, found = port, true
		}
	}
	return best, found
}

package infrastructure

import (
	"errors"
	"testing"
)

// envMap fabrique une lookupEnv de test.
func envMap(values map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		v, ok := values[key]
		return v, ok
	}
}

// fileMap fabrique un readFile de test qui refuse les chemins non listes
// (notamment ".env" absent).
func fileMap(files map[string]string) func(string) ([]byte, error) {
	return func(path string) ([]byte, error) {
		if content, ok := files[path]; ok {
			return []byte(content), nil
		}
		return nil, errors.New("absent: " + path)
	}
}

// noEnv est une environnement vide : aucune source ne repond.
func noEnv() func(string) (string, bool) { return envMap(nil) }

// noFiles est un disque sans .env.
func noFiles() func(string) ([]byte, error) { return fileMap(nil) }

// TestResolvePortDrapeauExplicite : --port passe explicitement gagne sur
// BRIDGE_PORT — c'est le cas du conteneur, dont la CMD porte "--port 8080"
// tandis que le meme .env expose BRIDGE_PORT=4000 cote hotte.
func TestResolvePortDrapeauExplicite(t *testing.T) {
	env := envMap(map[string]string{"BRIDGE_PORT": "4000"})
	files := fileMap(map[string]string{".env": "BRIDGE_PORT=4000"})

	if got := ResolvePort(8080, true, env, files); got != 8080 {
		t.Errorf("drapeau explicite: port = %d, attendu 8080", got)
	}
	// Sans drapeau, la meme configuration donne BRIDGE_PORT.
	if got := ResolvePort(8080, false, env, files); got != 4000 {
		t.Errorf("sans drapeau: port = %d, attendu 4000 (BRIDGE_PORT)", got)
	}
}

// TestResolvePortPriorites : environnement avant fichier .env, fichier .env
// avant repli 8080.
func TestResolvePortPriorites(t *testing.T) {
	dotEnv := fileMap(map[string]string{".env": "BRIDGE_PORT=4100"})

	if got := ResolvePort(8080, false, envMap(map[string]string{"BRIDGE_PORT": "4200"}), dotEnv); got != 4200 {
		t.Errorf("environnement: port = %d, attendu 4200", got)
	}
	if got := ResolvePort(8080, false, noEnv(), dotEnv); got != 4100 {
		t.Errorf("fichier .env: port = %d, attendu 4100", got)
	}
	if got := ResolvePort(8080, false, noEnv(), noFiles()); got != DefaultPort {
		t.Errorf("repli: port = %d, attendu %d", got, DefaultPort)
	}
}

// TestResolvePortValeursInvalidesIgnorees : une source malforme ne bloque
// pas le demarrage, on passe a la suivante.
func TestResolvePortValeursInvalidesIgnorees(t *testing.T) {
	for _, raw := range []string{"", "  ", "abc", "0", "-1", "70000", "4000 # commentaire"} {
		env := envMap(map[string]string{"BRIDGE_PORT": raw})
		files := fileMap(map[string]string{".env": "BRIDGE_PORT=4300"})
		if got := ResolvePort(8080, false, env, files); got != 4300 {
			t.Errorf("BRIDGE_PORT=%q invalide: port = %d, attendu 4300 (fichier .env)", raw, got)
		}
	}
	files := fileMap(map[string]string{".env": "BRIDGE_PORT=nonplus"})
	if got := ResolvePort(8080, false, noEnv(), files); got != DefaultPort {
		t.Errorf(".env invalide: port = %d, attendu %d", got, DefaultPort)
	}
}

// TestResolvePortLitLeBonFichier : c'est bien ".env" depuis le repertoire
// de travail qui est lu.
func TestResolvePortLitLeBonFichier(t *testing.T) {
	var paths []string
	readFile := func(path string) ([]byte, error) {
		paths = append(paths, path)
		return []byte("BRIDGE_PORT=4400"), nil
	}
	if got := ResolvePort(8080, false, noEnv(), readFile); got != 4400 {
		t.Errorf("port = %d, attendu 4400", got)
	}
	if len(paths) != 1 || paths[0] != ".env" {
		t.Errorf("fichiers lus = %v, attendu [.env]", paths)
	}
}

// TestPortFromDotEnv : formes reelles d'un .env — commentaires, export,
// guillemets simples et doubles, espaces, autres cles ignorees, derniere
// affectation valide gagnante.
func TestPortFromDotEnv(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want int
		ok   bool
	}{
		{"simple", "BRIDGE_PORT=4000", 4000, true},
		{"espaces", "  BRIDGE_PORT = 4001  ", 4001, true},
		{"export", "export BRIDGE_PORT=4002", 4002, true},
		{"guillemets doubles", `BRIDGE_PORT="4003"`, 4003, true},
		{"guillemets simples", `BRIDGE_PORT='4004'`, 4004, true},
		{"commentaire ignore", "# BRIDGE_PORT=1234", 0, false},
		{"autre cle ignoree", "OLLAMA_HOST=http://localhost:11434", 0, false},
		{"cle sans valeur", "BRIDGE_PORT=", 0, false},
		{"valeur invalide", "BRIDGE_PORT=abc", 0, false},
		{"plage", "BRIDGE_PORT=70000", 0, false},
		{"derniere valide gagne", "BRIDGE_PORT=4005\nBRIDGE_PORT=4006", 4006, true},
		{"plusieurs lignes", "# header\nBIFROST_MASTER_KEY=\"x\"\n\nBRIDGE_PORT=4007\nOTHER=1", 4007, true},
		{"crlf", "BRIDGE_PORT=4008\r\nOTHER=1", 4008, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := PortFromDotEnv([]byte(c.in))
			if ok != c.ok || (ok && got != c.want) {
				t.Errorf("PortFromDotEnv(%q) = (%d, %v), attendu (%d, %v)", c.in, got, ok, c.want, c.ok)
			}
		})
	}
}

// TestPortFromValue : bornes de la validation.
func TestPortFromValue(t *testing.T) {
	cases := []struct {
		in   string
		want int
		ok   bool
	}{
		{"4000", 4000, true},
		{" 4000 ", 4000, true},
		{"1", 1, true},
		{"65535", 65535, true},
		{"", 0, false},
		{"0", 0, false},
		{"65536", 0, false},
		{"http", 0, false},
	}
	for _, c := range cases {
		got, ok := PortFromValue(c.in)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("PortFromValue(%q) = (%d, %v), attendu (%d, %v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

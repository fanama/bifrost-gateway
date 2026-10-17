package main

import (
	"context"
	"flag"
	"log"
	"os"

	"bridge-gateway/infrastructure"
)

const generalProjectID = "project-general"

// appOptions carries the CLI-provided startup settings.
type appOptions struct {
	configPath string
	dbPath     string
	legacyDir  string
	port       int
	portSet    bool
}

func main() {
	configPath := flag.String("config", "config.yaml", "path to config file")
	port := flag.Int("port", 8080, "server port (BRIDGE_PORT from .env, then 8080)")
	dbPath := flag.String("db", "data/bridge.db", "path to the SQLite database file")
	legacyDir := flag.String("import-json", "data", "directory holding legacy JSON stores to migrate on first start (empty to skip)")
	flag.Parse()

	portSet := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "port" {
			portSet = true
		}
	})

	log.SetFlags(log.LstdFlags | log.Lshortfile)
	opts := appOptions{
		configPath: *configPath,
		dbPath:     *dbPath,
		legacyDir:  *legacyDir,
		port:       *port,
		portSet:    portSet,
	}
	if err := run(opts); err != nil {
		log.Fatalf("%v", err)
	}
}

// run resolves the effective port, builds the application, and serves until it
// exits. Construction is delegated to app.go.
func run(opts appOptions) error {
	port := infrastructure.ResolvePort(opts.port, opts.portSet, os.LookupEnv, os.ReadFile)
	log.Printf("Starting Bridge Gateway (BifrostAI + HTMX UI) on port %d", port)

	app, err := newGatewayApp(context.Background(), opts)
	if err != nil {
		return err
	}
	defer app.close()
	return app.serve(port)
}

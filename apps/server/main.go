package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/KevinBonnoron/viyu/apps/server/internal/app"
)

func main() {
	// Handled before app.New: a probe must not open the PocketBase data directory.
	if len(os.Args) == 2 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck())
	}
	server := app.New(app.Options{DataDir: os.Getenv("VIYU_DATA_DIR"), UIDir: os.Getenv("VIYU_UI_DIR")})
	if err := server.Start(); err != nil {
		log.Fatal(err)
	}
}

// healthcheck probes /api/health for container orchestrators. VIYU_HEALTHCHECK_URL overrides the
// address when the server listens elsewhere than the image default.
func healthcheck() int {
	url := os.Getenv("VIYU_HEALTHCHECK_URL")
	if url == "" {
		url = "http://127.0.0.1:8090/api/health"
	}
	client := http.Client{Timeout: 3 * time.Second}
	response, err := client.Get(url)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		fmt.Fprintln(os.Stderr, "health:", response.Status)
		return 1
	}
	return 0
}

package main

import (
	"log"

	"github.com/nxhai/vclaw/internal/app"
)

func main() {
	application, err := app.New()
	if err != nil {
		log.Fatalf("bootstrap failed: %v", err)
	}

	if err := application.Run(); err != nil {
		log.Fatalf("run failed: %v", err)
	}
}

package main

import (
	"log"
	"notificator/internal/webui"
)

func main() {
	// Backend address - in production, use environment variable
	backendAddress := "localhost:50051"
	
	router := webui.SetupRouter(backendAddress)
	
	log.Println("Starting WebUI server on :8081")
	log.Println("Backend address:", backendAddress)
	log.Println("Visit http://localhost:8081 to view the WebUI")
	
	if err := router.Run(":8081"); err != nil {
		log.Fatal("Failed to start server:", err)
	}
}
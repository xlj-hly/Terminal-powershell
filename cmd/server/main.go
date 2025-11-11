package main

import (
	"net/http"

	"terminal/internal/config"
	"terminal/internal/routes"
	"terminal/internal/server"
)

func main() {
	mux := routes.Setup()

	srv := &http.Server{
		Addr:    config.HOST + ":" + config.PORT,
		Handler: mux,
	}

	server.Start(srv)
	server.WaitForShutdown()
	server.Shutdown(srv)
}

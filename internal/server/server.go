package server

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"terminal/internal/config"
	"terminal/internal/handlers"
	"terminal/pkg/utils"
)

func Start(srv *http.Server) {
	go func() {
		localIP := utils.GetLocalIP()
		log.Println("========================================")
		log.Println("服务器已启动")
		log.Printf("HTTP 地址: http://%s:%s", localIP, config.PORT)
		log.Printf("WebSocket 地址: ws://%s:%s/ws", localIP, config.PORT)
		log.Printf("监听端口: %s", config.PORT)
		log.Println("========================================")

		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("服务器启动失败: %v", err)
		}
	}()
}

func Shutdown(srv *http.Server) {
	log.Println("\n正在关闭服务器...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	handlers.ShutdownAll()

	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("服务器关闭失败: %v", err)
	} else {
		log.Println("服务器已关闭")
	}
}

func WaitForShutdown() {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
}


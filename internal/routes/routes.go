package routes

import (
	"encoding/json"
	"log"
	"net/http"

	"terminal/internal/handlers"

	"github.com/gorilla/websocket"
)

const apiPrefix = "/api"

// handleMethod 限制只允许指定的 HTTP 方法
func handleMethod(method string, handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != method {
			http.NotFound(w, r)
			return
		}
		handler(w, r)
	}
}

func registerAPIRoutes(mux *http.ServeMux) {
	mux.HandleFunc(apiPrefix+"/command", handleMethod(http.MethodPost, handlers.HandleCommand))
}

func Setup() *http.ServeMux {
	// 创建路由
	mux := http.NewServeMux()

	// 根路径
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"message": "Terminal 服务器运行中",
			"version": "1.0.0",
		})
	})

	// 注册 API 路由
	registerAPIRoutes(mux)

	// WebSocket 路由
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("WebSocket 升级失败: %v", err)
			return
		}
		handlers.HandleWebSocket(conn)
	})

	return mux
}

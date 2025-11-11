//go:build windows
// +build windows

package handlers

import (
	"encoding/json"
	"io"
	"log"
	"os"
	"sync"

	"github.com/UserExistsError/conpty"
	"github.com/gorilla/websocket"
)

type Connection struct {
	ws     *websocket.Conn
	cpty   *conpty.ConPty
	mu     sync.Mutex
	closed bool
}

var (
	connections = make(map[*Connection]struct{})
	connMu      sync.RWMutex
)

func HandleWebSocket(ws *websocket.Conn) {
	// 检查 ConPTY 是否可用
	if !conpty.IsConPtyAvailable() {
		log.Printf("ConPTY 不可用，Windows 版本可能过低")
		ws.Close()
		return
	}

	// 设置工作目录
	homeDir := os.Getenv("USERPROFILE")
	if homeDir == "" {
		homeDir = os.Getenv("HOME")
	}
	if homeDir == "" {
		homeDir, _ = os.Getwd()
	}

	// 构建命令字符串
	commandLine := "powershell.exe"

	// 创建 ConPTY
	cpty, err := conpty.Start(
		commandLine,
		conpty.ConPtyDimensions(80, 24),
		conpty.ConPtyWorkDir(homeDir),
		conpty.ConPtyEnv(os.Environ()),
	)
	if err != nil {
		log.Printf("创建 ConPTY 失败: %v", err)
		ws.Close()
		return
	}

	conn := &Connection{
		ws:   ws,
		cpty: cpty,
	}

	// 添加到连接集合
	connMu.Lock()
	connections[conn] = struct{}{}
	connMu.Unlock()

	log.Printf("新的终端连接: %s，进程ID: %d", ws.RemoteAddr(), cpty.Pid())

	// ConPTY 输出 -> WebSocket
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := conn.cpty.Read(buf)
			if err != nil {
				if err != io.EOF {
					log.Printf("读取 ConPTY 输出失败: %v", err)
				}
				conn.Close()
				return
			}

			conn.mu.Lock()
			if !conn.closed && conn.ws != nil {
				if err := conn.ws.WriteMessage(websocket.TextMessage, buf[:n]); err != nil {
					conn.mu.Unlock()
					conn.Close()
					return
				}
			}
			conn.mu.Unlock()
		}
	}()

	// WebSocket 输入 -> ConPTY
	for {
		messageType, data, err := ws.ReadMessage()
		if err != nil {
			conn.Close()
			return
		}

		if messageType == websocket.TextMessage || messageType == websocket.BinaryMessage {
			conn.handleMessage(data)
		}
	}
}

func (c *Connection) handleMessage(data []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed || c.cpty == nil {
		return
	}

	// 尝试解析 JSON（resize 消息）
	var msg struct {
		Type string `json:"type"`
		Cols int    `json:"cols"`
		Rows int    `json:"rows"`
	}

	if err := json.Unmarshal(data, &msg); err == nil && msg.Type == "resize" && msg.Cols > 0 && msg.Rows > 0 {
		// 窗口大小调整
		if err := c.cpty.Resize(msg.Cols, msg.Rows); err != nil {
			log.Printf("调整窗口大小失败: %v", err)
		}
		return
	}

	// 普通输入，写入 ConPTY
	c.cpty.Write(data)
}

func (c *Connection) Close() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	c.mu.Unlock()

	// 从连接集合移除
	connMu.Lock()
	delete(connections, c)
	connMu.Unlock()

	// 关闭 ConPTY（会自动终止进程）
	if c.cpty != nil {
		c.cpty.Close()
	}

	// 关闭 WebSocket
	if c.ws != nil {
		c.ws.Close()
	}

	log.Println("终端连接已关闭")
}

func ShutdownAll() {
	connMu.Lock()
	defer connMu.Unlock()

	for conn := range connections {
		conn.Close()
	}
}

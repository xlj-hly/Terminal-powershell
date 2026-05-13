//go:build windows
// +build windows

package handlers

import (
	"encoding/json"
	"io"
	"log"
	"os"
	"sync"
	"time"

	"github.com/UserExistsError/conpty"
	"github.com/gorilla/websocket"
)

const (
	maxConnections = 100              // 最大连接数
	readTimeout    = 60 * time.Second // 读取超时时间
	pingInterval   = 30 * time.Second // 心跳间隔时间
)

type Connection struct {
	ws     *websocket.Conn // WebSocket 连接
	cpty   *conpty.ConPty  // ConPTY 连接
	mu     sync.Mutex      // 互斥锁
	closed bool            // 连接是否已关闭
}

var (
	connections = make([]*Connection, 0, maxConnections) // 连接集合
	connMu      sync.RWMutex                             // 互斥锁
)

// 初始化时启动连接清理 goroutine
func init() {
	go cleanupConnections() // 启动连接清理 goroutine
}

// 定期清理无效连接
func cleanupConnections() {
	ticker := time.NewTicker(5 * time.Minute) // 5 分钟清理一次
	defer ticker.Stop()
	for range ticker.C {
		connMu.Lock()
		validConnections := make([]*Connection, 0, len(connections)) // 有效连接集合
		for _, conn := range connections {
			conn.mu.Lock()
			if !conn.closed {
				validConnections = append(validConnections, conn) // 添加到有效连接集合
			}
			conn.mu.Unlock()
		}
		connections = validConnections // 更新连接集合
		connMu.Unlock()
	}
}

func HandleWebSocket(ws *websocket.Conn) {
	// 检查连接数限制，如果达到上限则移除最旧的连接（FIFO）
	connMu.Lock()
	if len(connections) >= maxConnections {
		oldestConn := connections[0]  // 最旧的连接
		connections = connections[1:] // 移除最旧的连接
		connMu.Unlock()
		log.Printf("连接数已达上限 (%d)，关闭最旧连接为新连接腾出空间", maxConnections)
		oldestConn.Close() // 关闭最旧的连接
	} else {
		connMu.Unlock() // 释放互斥锁
	}

	// 检查 ConPTY 是否可用
	if !conpty.IsConPtyAvailable() {
		log.Printf("ConPTY 不可用，Windows 版本可能过低")
		ws.Close()
		return
	}

	// 设置工作目录
	homeDir := os.Getenv("USERPROFILE") // 获取用户主目录
	if homeDir == "" {
		homeDir = os.Getenv("HOME") // 获取用户主目录
	}
	if homeDir == "" {
		homeDir, _ = os.Getwd() // 获取当前工作目录
	}

	// 创建 ConPTY
	cpty, err := conpty.Start(
		"powershell.exe",                // 创建 PowerShell 进程
		conpty.ConPtyDimensions(80, 24), // 设置窗口大小
		conpty.ConPtyWorkDir(homeDir),   // 设置工作目录
		conpty.ConPtyEnv(os.Environ()),  // 设置环境变量
	)
	if err != nil {
		log.Printf("创建 ConPTY 失败: %v", err)
		ws.Close()
		return
	}

	conn := &Connection{ // 创建连接
		ws:   ws,
		cpty: cpty,
	}

	// 添加到连接集合（追加到末尾，保持 FIFO 顺序）
	connMu.Lock()
	connections = append(connections, conn)
	connMu.Unlock()

	log.Printf("新的终端连接: %s，进程ID: %d", ws.RemoteAddr(), cpty.Pid())

	// 设置 WebSocket 读取超时和 ping/pong
	ws.SetReadDeadline(time.Now().Add(readTimeout))
	ws.SetPongHandler(func(string) error {
		ws.SetReadDeadline(time.Now().Add(readTimeout))
		return nil
	})

	// 心跳检测：定期发送 ping
	go func() {
		ticker := time.NewTicker(pingInterval)
		defer ticker.Stop()  // 停止心跳检测
		for range ticker.C { // 定期发送 ping
			conn.mu.Lock()                     // 锁定互斥锁
			if conn.closed || conn.ws == nil { // 如果连接已关闭或 WebSocket 连接已关闭
				conn.mu.Unlock()
				return
			}
			if err := conn.ws.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(5*time.Second)); err != nil {
				conn.mu.Unlock()
				conn.Close()
				return
			}
			conn.mu.Unlock()
		}
	}()

	// ConPTY 输出 -> WebSocket
	go func() {
		buf := make([]byte, 32*1024) // 创建缓冲区
		for {
			n, err := conn.cpty.Read(buf)
			if err != nil {
				conn.mu.Lock()
				alreadyClosed := conn.closed // 获取连接是否已关闭
				conn.mu.Unlock()

				if !alreadyClosed && err != io.EOF { // 如果连接未关闭且不是 EOF
					log.Printf("读取 ConPTY 输出失败: %v", err)
					conn.Close()
				}
				return
			}

			conn.mu.Lock()
			if conn.closed || conn.ws == nil { // 如果连接已关闭或 WebSocket 连接已关闭
				conn.mu.Unlock()
				return
			}
			if err := conn.ws.WriteMessage(websocket.TextMessage, buf[:n]); err != nil { // 写入 WebSocket
				conn.mu.Unlock()
				conn.Close()
				return
			}
			conn.mu.Unlock()
		}
	}()

	// WebSocket 输入 -> ConPTY
	for {
		// 设置读取超时，防止半开连接
		ws.SetReadDeadline(time.Now().Add(readTimeout))
		messageType, data, err := ws.ReadMessage()
		if err != nil {
			// 使用更精确的错误检查
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) { // 如果 WebSocket 意外关闭
				log.Printf("WebSocket 意外关闭: %v", err)
			} else if netErr, ok := err.(interface{ Timeout() bool }); ok && netErr.Timeout() { // 如果 WebSocket 读取超时
				log.Printf("WebSocket 读取超时，关闭连接: %v", err)
			} else if err != io.EOF {
				log.Printf("WebSocket 读取错误: %v", err)
			}
			conn.Close()
			return
		}

		if messageType == websocket.TextMessage || messageType == websocket.BinaryMessage { // 如果消息类型为文本或二进制
			conn.handleMessage(data)
		}
	}
}

func (c *Connection) handleMessage(data []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed || c.cpty == nil { // 如果连接已关闭或 ConPTY 连接已关闭
		return
	}

	// 尝试解析 JSON（resize 消息）
	var msg struct {
		Type string `json:"type"`
		Cols int    `json:"cols"`
		Rows int    `json:"rows"`
	}

	if err := json.Unmarshal(data, &msg); err == nil && msg.Type == "resize" && msg.Cols > 0 && msg.Rows > 0 { // 如果消息类型为 resize
		// 窗口大小调整
		if err := c.cpty.Resize(msg.Cols, msg.Rows); err != nil {
			log.Printf("调整窗口大小失败: %v", err)
		}
		return
	}

	// 普通输入，写入 ConPTY
	if _, err := c.cpty.Write(data); err != nil {
		log.Printf("写入 ConPTY 失败: %v", err)
		// 如果写入失败，可能需要关闭连接
		go c.Close()
	}
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
	for i, conn := range connections {
		if conn == c {
			connections = append(connections[:i], connections[i+1:]...)
			break
		}
	}
	connMu.Unlock()

	// 先关闭 ConPTY（会自动终止进程，不需要 taskkill）
	// 这会触发读取 goroutine 检测到错误并退出
	// 根据 conpty 源码，win32ClosePseudoConsole 的注释明确说明：
	// this kills the attached process. there is no return value.
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
	conns := connections
	connections = nil
	connMu.Unlock()

	for _, conn := range conns {
		conn.Close()
	}
}

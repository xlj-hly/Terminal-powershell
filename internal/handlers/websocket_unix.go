//go:build !windows
// +build !windows

package handlers

import (
	"encoding/json"
	"io"
	"log"
	"os"
	"os/exec"
	"sync"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"
)

type Connection struct {
	ws     *websocket.Conn
	cmd    *exec.Cmd
	ptmx   *os.File
	mu     sync.Mutex
	closed bool
}

var (
	connections = make(map[*Connection]struct{})
	connMu      sync.RWMutex
)

func HandleWebSocket(ws *websocket.Conn) {
	// 创建 bash 进程
	cmd := exec.Command("bash")

	// 设置工作目录
	homeDir := os.Getenv("HOME")
	if homeDir == "" {
		homeDir, _ = os.Getwd()
	}
	cmd.Dir = homeDir
	cmd.Env = os.Environ()

	// 创建 pty
	ptmx, err := pty.Start(cmd)
	if err != nil {
		log.Printf("创建 pty 失败: %v", err)
		ws.Close()
		return
	}

	// 设置初始窗口大小
	pty.Setsize(ptmx, &pty.Winsize{
		Rows: 24,
		Cols: 80,
	})

	conn := &Connection{
		ws:   ws,
		cmd:  cmd,
		ptmx: ptmx,
	}

	// 添加到连接集合
	connMu.Lock()
	connections[conn] = struct{}{}
	connMu.Unlock()

	log.Printf("新的终端连接: %s，进程ID: %d", ws.RemoteAddr(), cmd.Process.Pid)

	// pty 输出 -> WebSocket
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := conn.ptmx.Read(buf)
			if err != nil {
				if err != io.EOF {
					log.Printf("读取 pty 输出失败: %v", err)
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

	// WebSocket 输入 -> pty
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

	if c.closed || c.ptmx == nil {
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
		pty.Setsize(c.ptmx, &pty.Winsize{
			Rows: uint16(msg.Rows),
			Cols: uint16(msg.Cols),
		})
		return
	}

	// 普通输入，写入 pty
	c.ptmx.Write(data)
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

	// 关闭 pty
	if c.ptmx != nil {
		c.ptmx.Close()
	}

	// 终止进程
	var pid int
	if c.cmd != nil && c.cmd.Process != nil {
		pid = c.cmd.Process.Pid
		c.cmd.Process.Kill()
		c.cmd.Wait()
	}

	// 关闭 WebSocket
	if c.ws != nil {
		c.ws.Close()
	}

	if pid > 0 {
		log.Printf("终端连接已关闭: 进程ID %d", pid)
	} else {
		log.Println("终端连接已关闭")
	}
}

func ShutdownAll() {
	connMu.Lock()
	defer connMu.Unlock()

	for conn := range connections {
		conn.Close()
	}
}

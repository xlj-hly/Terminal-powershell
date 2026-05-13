# Terminal Go

Go 语言实现的终端服务，提供 WebSocket 终端和命令执行接口。

## 功能特性

- ✅ WebSocket 实时终端交互
- ✅ HTTP API 命令执行
- ✅ 跨平台支持（Windows/Linux/macOS）
- ✅ 优雅关闭机制
- ✅ 标准 Go 项目结构

## 项目结构

```
terminal/
├── cmd/
│   └── server/
│       └── main.go          # 程序入口
├── internal/                # 私有代码（外部无法导入）
│   ├── config/
│   │   └── config.go        # 配置常量
│   ├── handlers/            # HTTP/WebSocket 处理器
│   │   ├── command.go       # 命令执行处理器
│   │   └── websocket.go     # WebSocket 终端处理器
│   ├── routes/
│   │   └── routes.go        # 路由设置
│   └── server/
│       └── server.go        # 服务器启动/关闭逻辑
├── pkg/                     # 公共库（可被外部导入）
│   └── utils/
│       ├── encoding.go      # 编码转换工具
│       └── utils.go         # 通用工具函数
├── go.mod
├── go.sum
└── README.md
```

## 快速开始

### 安装依赖

```bash
go mod tidy
```

### 编译

```bash
# Windows
go build -o terminal.exe ./cmd/server

# Linux/macOS
go build -o terminal ./cmd/server
```

### 运行

```bash
# Windows
./terminal.exe

# Linux/macOS
./terminal
```

或直接运行：

```bash
go run ./cmd/server/main.go
```

## 配置

默认配置（可在 `internal/config/config.go` 中修改）：

- **端口**: 3000
- **主机**: 0.0.0.0

## API 接口

### 1. 根路径

**GET** `/`

返回服务器状态信息。

**响应示例：**
```json
{
  "message": "Terminal 服务器运行中",
  "version": "1.0.0"
}
```

### 2. 执行命令

**POST** `/api/command`

执行系统命令并返回结果。

**请求体：**
```json
{
  "command": "echo Hello World"
}
```

**响应：**
- 成功：返回命令输出（状态码 200）
- 失败：返回错误信息（状态码 500）

**示例：**
```bash
curl -X POST http://localhost:3000/api/command \
  -H "Content-Type: application/json" \
  -d '{"command": "echo Hello World"}'
```

### 3. WebSocket 终端

**WebSocket** `ws://localhost:3000/ws`

建立 WebSocket 连接后，可以：

- **发送文本**：直接发送命令输入
- **发送 JSON**：调整终端窗口大小
  ```json
  {
    "type": "resize",
    "cols": 80,
    "rows": 24
  }
  ```

**连接示例（JavaScript）：**
```javascript
const ws = new WebSocket('ws://localhost:3000/ws');

ws.onopen = () => {
  console.log('连接已建立');
  // 发送命令
  ws.send('ls -la\n');
};

ws.onmessage = (event) => {
  console.log('收到输出:', event.data);
};

ws.onerror = (error) => {
  console.error('错误:', error);
};

ws.onclose = () => {
  console.log('连接已关闭');
};
```

## 依赖

- `github.com/creack/pty` - 伪终端支持
- `github.com/gorilla/websocket` - WebSocket 支持
- `golang.org/x/text` - 编码转换

## 注意事项

- Windows 系统使用 PowerShell，Linux/macOS 使用 bash
- Windows 输出会自动进行 GBK 到 UTF-8 的编码转换
- 命令执行有 30 秒超时限制
- WebSocket 连接断开时会自动清理相关进程

## 开发

项目采用标准 Go 项目布局：

- `cmd/` - 可执行程序入口
- `internal/` - 私有代码，外部无法导入
- `pkg/` - 公共库，可被其他项目导入

## License

MIT

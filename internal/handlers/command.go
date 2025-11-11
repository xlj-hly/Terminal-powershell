package handlers

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"terminal/pkg/utils"
)

// 函数参数说明：
//
//	w http.ResponseWriter: 用于向客户端（前端）写响应数据
//	   - 通过 w.Write() 写入响应内容
//	   - 通过 w.WriteHeader() 设置状态码
//	   - 这是接口类型，Go 会自动处理
//
//	r *http.Request: 包含客户端（前端）发送的 HTTP 请求信息
//	   - * 表示指针（pointer），指向 Request 对象的内存地址
//	   - 通过 r.Body 读取请求体数据
//	   - 通过 r.Header 读取请求头
//	   - 使用指针的原因：Request 对象很大，传指针避免复制整个对象，提高性能
func HandleCommand(w http.ResponseWriter, r *http.Request) {
	// ============================================
	// 【重要】Go 是静态类型语言，与 Python/JavaScript 等解释型语言的区别：
	//
	// 解释型语言（如 Python/JavaScript）：
	//   data = json.loads(request.body)  # 直接解析，data 是动态类型字典
	//   command = data['command']         # 直接访问，运行时才知道类型
	//
	// Go（静态类型）：
	//   必须先定义数据结构，告诉编译器每个字段的类型
	//   然后才能解析 JSON，解析时会进行类型检查和转换
	// ============================================

	// 定义请求结构体 - 这是 Go 接收 JSON 数据的方式
	//
	// 为什么需要定义结构体？
	//   1. Go 是静态类型，必须提前声明数据的"形状"（有哪些字段，什么类型）
	//   2. 结构体就像一个"模板"，告诉 Go 如何解析和存储 JSON 数据
	//   3. 解析时，Go 会按照这个模板创建新的内存空间来存储数据
	//
	// 结构体字段说明：
	//   - Command: 字段名（必须大写开头，Go 的可见性规则）
	//   - string: 字段类型（字符串类型）
	//   - `json:"command"`: JSON 标签，建立映射关系
	//      - JSON 中的字段名是 "command"（小写，前端发送的格式）
	//      - Go 结构体中的字段名是 Command（大写，代码中访问时使用）
	//      - 解析时：JSON 的 "command" → Go 的 Command
	var req struct {
		Command string `json:"command"`
	}
	// 此时 req 是一个空结构体：{Command: ""}
	// 它已经在内存中分配了空间，但 Command 字段是空字符串

	// ============================================
	// JSON 解析过程详解：
	//
	// 前端发送的数据（在 r.Body 中）：
	//   {"command": "dir"}  ← 这是 JSON 格式的字符串（字节流）
	//
	// 解析过程（json.NewDecoder().Decode()）：
	//   1. 从 r.Body 读取 JSON 字节流
	//   2. 解析 JSON 字符串，识别出字段名 "command" 和值 "dir"
	//   3. 根据 json:"command" 标签，找到对应的结构体字段 Command
	//   4. 将 JSON 中的字符串 "dir" 转换为 Go 的 string 类型
	//   5. 将转换后的值赋给 req.Command
	//
	// 解析后的结果：
	//   req.Command = "dir"  ← 这是 Go 的 string 类型，存储在内存中
	//
	// 重要：这是数据复制，不是引用
	//   - r.Body 中的原始数据不会被修改
	//   - req 是内存中的新对象，与 r.Body 完全独立
	// ============================================
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		// 如果解析失败（JSON 格式错误、类型不匹配等），返回 400 错误
		http.Error(w, "无效的请求体", http.StatusBadRequest)
		return
	}
	// 解析成功后，req.Command 现在包含了前端发送的命令字符串

	// 验证命令是否为空
	// 在解释型语言中可能是：if not command: ...
	// Go 中字符串为空就是空字符串 ""
	if req.Command == "" {
		http.Error(w, "未提供有效的命令", http.StatusBadRequest)
		return
	}

	// ============================================
	// Context（上下文）详解：
	//
	// Context 是什么？
	//   - Context 是 Go 中用于控制 goroutine（协程）生命周期和取消操作的机制
	//   - 可以传递取消信号、超时、截止时间等信息
	//   - 类似于"遥控器"，可以远程控制正在运行的任务
	//
	// context.WithTimeout() 的作用：
	//   - 创建一个带超时的上下文
	//   - context.Background(): 创建根上下文（空上下文，作为起点）
	//   - 30*time.Second: 设置 30 秒超时时间
	//   - 返回两个值：
	//     - ctx: 上下文对象，传递给需要控制的任务
	//     - cancel: 取消函数，可以手动取消任务
	//
	// defer cancel() 的作用：
	//   - defer: 延迟执行，函数结束时自动执行
	//   - cancel(): 释放上下文资源，避免内存泄漏
	//   - 即使任务正常完成，也要调用 cancel() 清理资源
	//
	// 为什么需要超时？
	//   - 防止命令执行时间过长（比如死循环、卡死）
	//   - 30 秒后自动取消命令执行，避免服务器资源被占用
	//   - 保护服务器不被恶意或错误的命令拖垮
	// ============================================
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel() // 函数结束时自动清理资源

	// ============================================
	// 创建命令执行对象 - 根据操作系统选择不同的命令解释器
	//
	// var cmd *exec.Cmd:
	//   - 声明一个命令对象变量（指针类型）
	//   - exec.Cmd 是 Go 中用于执行外部命令的结构体
	//   - * 表示指针，因为 Cmd 对象比较大，用指针更高效
	//
	// runtime.GOOS:
	//   - 获取当前运行的操作系统类型
	//   - 可能的值："windows"、"linux"、"darwin"（Mac）、"freebsd" 等
	//   - 用于判断是 Windows 还是其他系统（Linux/Mac）
	//
	// 为什么需要区分操作系统？
	//   - Windows 和 Linux/Mac 的命令解释器不同
	//   - Windows: 使用 PowerShell 或 cmd.exe
	//   - Linux/Mac: 使用 sh（shell）或 bash
	//   - 命令语法也不同，需要不同的参数格式
	//
	// exec.CommandContext() 参数说明：
	//   - 第一个参数 ctx: 上下文，用于控制超时和取消
	//   - 第二个参数: 要执行的程序（powershell.exe 或 sh）
	//   - 后续参数: 传递给程序的参数
	//
	// Windows 命令格式：
	//   exec.CommandContext(ctx, "powershell.exe", "-Command", req.Command)
	//   - powershell.exe: Windows PowerShell 解释器
	//   - "-Command": PowerShell 参数，表示执行后面的命令
	//   - req.Command: 前端发送的命令（如 "dir"、"Get-Process" 等）
	//   示例: powershell.exe -Command "dir"
	//
	// Linux/Mac 命令格式：
	//   exec.CommandContext(ctx, "sh", "-c", req.Command)
	//   - sh: Shell 解释器（Unix/Linux 系统的命令解释器）
	//   - "-c": sh 的参数，表示执行后面的命令字符串
	//   - req.Command: 前端发送的命令（如 "ls"、"ps aux" 等）
	//   示例: sh -c "ls -la"
	// ============================================
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		// Windows 系统：使用 PowerShell 执行命令
		cmd = exec.CommandContext(ctx, "powershell.exe", "-Command", req.Command)
	} else {
		// Linux/Mac 系统：使用 sh 执行命令
		cmd = exec.CommandContext(ctx, "sh", "-c", req.Command)
	}
	// 此时 cmd 已经准备好，但还没有执行，需要调用 cmd.Start() 才会真正执行

	// ============================================
	// 创建管道（Pipe）- 用于获取命令的输出
	//
	// 什么是管道？
	//   - 管道是进程间通信的机制，类似于"水管"
	//   - 命令的输出（stdout/stderr）通过管道流到我们的程序
	//   - 可以实时读取命令的输出，而不需要等命令执行完
	//
	// stdout（标准输出）：
	//   - 命令正常输出的内容（比如 "dir" 命令的文件列表）
	//   - 在解释型语言中，通常是 subprocess.run() 返回的 stdout
	//
	// stderr（标准错误）：
	//   - 命令的错误输出（比如命令执行失败的错误信息）
	//   - 在解释型语言中，通常是 subprocess.run() 返回的 stderr
	//
	// 为什么需要先创建管道再启动命令？
	//   - 必须在启动命令前创建管道，否则无法捕获输出
	//   - 管道是流式的，可以边执行边读取，不需要等命令结束
	// ============================================
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		http.Error(w, "创建 stdout 管道失败", http.StatusInternalServerError)
		return
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		http.Error(w, "创建 stderr 管道失败", http.StatusInternalServerError)
		return
	}

	// 启动命令执行
	// cmd.Start() 会启动一个新的进程来执行命令
	// 命令会在后台运行，不会阻塞当前代码
	if err := cmd.Start(); err != nil {
		http.Error(w, "进程启动失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	// 此时命令已经开始执行了，stdout 和 stderr 管道中会有数据流进来

	// ============================================
	// 并发读取命令输出 - 使用 Goroutine（协程）
	//
	// 什么是 Goroutine？
	//   - Goroutine 是 Go 的轻量级线程（协程）
	//   - 类似于 Python 的 threading 或 JavaScript 的 Promise
	//   - 可以同时执行多个任务，不阻塞主程序
	//
	// 为什么需要并发读取？
	//   - stdout 和 stderr 是两个独立的流，需要同时读取
	//   - 如果顺序读取，一个阻塞会导致另一个无法读取
	//   - 并发读取可以同时处理两个流，提高效率
	//
	// sync.WaitGroup 的作用：
	//   - 用于等待多个 goroutine 完成
	//   - wg.Add(1): 增加等待计数（表示启动了一个 goroutine）
	//   - wg.Done(): 减少等待计数（表示一个 goroutine 完成了）
	//   - wg.Wait(): 阻塞等待，直到所有 goroutine 完成
	//
	// []byte 是什么？
	//   - 字节切片，用于存储二进制数据（命令输出是字节流）
	//   - 类似于 Python 的 bytes 或 JavaScript 的 Uint8Array
	// ============================================
	var stdoutChunks, stderrChunks []byte // 用于存储读取到的数据
	var wg sync.WaitGroup                 // 用于等待所有 goroutine 完成

	// 启动 goroutine 收集 stdout（标准输出）
	wg.Add(1) // 增加等待计数：告诉 WaitGroup 有一个 goroutine 要等待
	go func() {
		// go func() 表示启动一个新的 goroutine（协程）并发执行
		// 这个函数会在后台运行，不会阻塞主程序
		defer wg.Done()              // 函数结束时调用，减少等待计数
		buf := make([]byte, 32*1024) // 创建 32KB 的缓冲区，用于每次读取
		for {
			// 循环读取数据，直到读取完所有数据
			n, err := stdout.Read(buf) // 从管道读取数据到缓冲区
			// n: 实际读取的字节数
			// err: 错误信息（如果有）
			if n > 0 {
				// 如果读取到了数据，追加到 stdoutChunks
				// append() 用于向切片添加元素
				// buf[:n] 表示只取前 n 个字节（因为缓冲区可能没填满）
				stdoutChunks = append(stdoutChunks, buf[:n]...)
			}
			if err == io.EOF {
				// EOF (End Of File) 表示数据读取完毕
				break // 退出循环
			}
			if err != nil {
				// 如果发生其他错误，记录日志并退出
				log.Printf("读取 stdout 失败: %v", err)
				break
			}
		}
	}()

	// 启动 goroutine 收集 stderr（标准错误）
	// 逻辑与上面相同，只是读取的是 stderr 管道
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 32*1024)
		for {
			n, err := stderr.Read(buf)
			if n > 0 {
				stderrChunks = append(stderrChunks, buf[:n]...)
			}
			if err == io.EOF {
				break
			}
			if err != nil {
				log.Printf("读取 stderr 失败: %v", err)
				break
			}
		}
	}()

	// 等待所有 goroutine 完成
	// 这会阻塞当前代码，直到两个 goroutine 都读取完数据
	wg.Wait()
	// 此时 stdoutChunks 和 stderrChunks 已经包含了所有输出数据

	// ============================================
	// 等待命令进程结束并获取退出码
	//
	// cmd.Wait() 的作用：
	//   - 等待命令进程执行完成
	//   - 返回进程的退出状态
	//   - 如果进程正常退出（退出码为 0），返回 nil
	//   - 如果进程异常退出（退出码不为 0），返回错误
	//
	// 退出码（Exit Code）：
	//   - 0: 命令执行成功
	//   - 非 0: 命令执行失败（不同的数字表示不同的错误）
	//   - 类似于 Python 的 sys.exit(0) 或 shell 的 $?
	//
	// 类型断言 err.(*exec.ExitError)：
	//   - 这是 Go 的类型断言语法
	//   - 尝试将 err 转换为 *exec.ExitError 类型
	//   - ok 为 true 表示转换成功，可以获取退出码
	//   - 类似于 Python 的 isinstance() 检查
	// ============================================
	exitCode := 0 // 默认退出码为 0（成功）
	if err := cmd.Wait(); err != nil {
		// 如果命令执行失败，尝试获取退出码
		// 类型断言：检查 err 是否是 *exec.ExitError 类型
		if exitError, ok := err.(*exec.ExitError); ok {
			// ok 为 true 表示是退出错误，可以获取退出码
			exitCode = exitError.ExitCode()
		}
		log.Printf("[子进程退出] PID: %d, 退出码: %d", cmd.Process.Pid, exitCode)
	} else {
		// 命令正常退出（退出码为 0）
		log.Printf("[子进程退出] PID: %d, 退出码: 0", cmd.Process.Pid)
	}

	// ============================================
	// 编码转换 - 处理不同操作系统的字符编码
	//
	// 为什么需要编码转换？
	//   - Windows 系统默认使用 GBK 编码（中文 Windows）
	//   - Linux/Mac 系统默认使用 UTF-8 编码
	//   - 前端通常使用 UTF-8 编码
	//   - 需要统一转换为 UTF-8，前端才能正确显示
	//
	// string() 转换：
	//   - 将 []byte（字节切片）转换为 string（字符串）
	//   - 在 Linux/Mac 上，字节已经是 UTF-8，直接转换即可
	//   - 在 Windows 上，字节是 GBK，需要先转换再转字符串
	// ============================================
	var output, errorMsg string
	if runtime.GOOS == "windows" {
		// Windows 系统：需要从 GBK 转换为 UTF-8
		output = utils.ConvertGBKToUTF8(stdoutChunks)
		errorMsg = utils.ConvertGBKToUTF8(stderrChunks)
	} else {
		// Linux/Mac 系统：直接转换为字符串（已经是 UTF-8）
		output = string(stdoutChunks)
		errorMsg = string(stderrChunks)
	}

	// ============================================
	// 组装最终结果 - 使用 strings.Builder 高效拼接字符串
	//
	// strings.Builder 是什么？
	//   - Go 中用于高效拼接字符串的工具
	//   - 类似于 Python 的 "".join() 或 JavaScript 的 Array.join()
	//   - 比直接用 + 拼接字符串更高效（避免多次内存分配）
	//
	// 为什么先放 errorMsg 再放 output？
	//   - 错误信息通常更重要，放在前面
	//   - 如果两者都有，用换行符分隔
	// ============================================
	var result strings.Builder
	if errorMsg != "" {
		result.WriteString(errorMsg) // 先写入错误信息
	}
	if output != "" {
		if result.Len() > 0 {
			// 如果已经有错误信息，先加一个换行符
			result.WriteString("\n")
		}
		result.WriteString(output) // 再写入正常输出
	}

	// ============================================
	// 设置 HTTP 响应状态码
	//
	// HTTP 状态码：
	//   - 200 (StatusOK): 请求成功
	//   - 500 (StatusInternalServerError): 服务器内部错误
	//
	// 判断逻辑：
	//   - 如果退出码不为 0 或是有错误信息，返回 500
	//   - 否则返回 200
	// ============================================
	statusCode := http.StatusOK // 默认 200
	if exitCode != 0 || errorMsg != "" {
		statusCode = http.StatusInternalServerError // 500
	}

	// 发送 HTTP 响应
	w.WriteHeader(statusCode)        // 设置状态码
	w.Write([]byte(result.String())) // 写入响应内容
	// result.String() 将 Builder 转换为字符串
	// []byte(...) 将字符串转换为字节切片（Write 需要字节切片）
}

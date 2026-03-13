package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"sop-chat/internal/api"
	"sop-chat/internal/client"
	"sop-chat/internal/config"

	"github.com/joho/godotenv"
	"gopkg.in/lumberjack.v2"
)

const (
	envDaemonMode = "SOP_CHAT_DAEMON" // 标识当前进程是 daemon 子进程
	envAdminToken = "SOP_ADMIN_TOKEN" // daemon 子进程继承 token，与父进程一致
	envAdminPort  = "SOP_ADMIN_PORT"  // daemon 子进程继承父进程探测到的端口

	adminURLName = "sop-chat-server.url" // 管理 UI URL 文件名（0600，仅属主可读）
	adminPIDName = "sop-chat-server.pid" // PID 文件名
	adminLogName = "sop-chat-server.log" // 日志文件名
)

func main() {
	// stop 子命令：读取 PID 文件并终止守护进程
	if len(os.Args) >= 2 && os.Args[1] == "stop" {
		stopDaemon()
		return
	}

	// adminurl 子命令：通过 Unix socket 向运行中的 daemon 实时查询配置管理 UI 地址
	if len(os.Args) >= 2 && os.Args[1] == "adminurl" {
		queryAdminURL()
		return
	}

	// 解析命令行参数
	var configPath string
	var showHelp bool
	var noDaemon bool
	flag.StringVar(&configPath, "config", "", "配置文件路径（默认: config.yaml 或 CONFIG_PATH 环境变量）")
	flag.StringVar(&configPath, "c", "", "配置文件路径（-config 的简写）")
	flag.BoolVar(&showHelp, "help", false, "显示帮助信息")
	flag.BoolVar(&showHelp, "h", false, "显示帮助信息")
	flag.BoolVar(&noDaemon, "no-daemon", false, "前台运行，不进入守护进程模式")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "SOP Chat API Server\n\n")
		fmt.Fprintf(os.Stderr, "用法: %s [子命令] [选项]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "子命令:\n")
		fmt.Fprintf(os.Stderr, "  stop        停止后台守护进程\n")
		fmt.Fprintf(os.Stderr, "  adminurl    打印当前运行实例的配置管理 UI 地址\n\n")
		fmt.Fprintf(os.Stderr, "选项:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\n示例:\n")
		fmt.Fprintf(os.Stderr, "  %s -config /path/to/config.yaml\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s -c ./config.yaml\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s stop\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s adminurl\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\n注意: 端口配置请在 config.yaml 的 global.port 中设置\n")
	}
	flag.Parse()

	if showHelp {
		flag.Usage()
		os.Exit(0)
	}

	// 首先加载当前目录的 .env 文件
	if err := godotenv.Load(); err != nil {
		log.Println("提示: 未找到 .env 文件，将使用系统环境变量")
	}

	// 确定配置文件路径（优先级: 命令行参数 > 环境变量 > 默认值）
	if configPath == "" {
		configPath = os.Getenv("CONFIG_PATH")
		if configPath == "" {
			configPath = "config.yaml"
		}
	} else {
		os.Setenv("CONFIG_PATH", configPath)
		log.Printf("使用配置文件: %s", configPath)
	}

	// 加载统一配置（如果文件不存在则自动创建默认配置）
	var finalPort int
	var isFirstRun bool
	unifiedConfig, actualPath, err := config.LoadConfig(configPath)
	if err != nil {
		isFirstRun = true
		log.Printf("提示: 未找到配置文件 %s，将自动创建默认配置", configPath)
		unifiedConfig = config.DefaultConfig()
		actualPath = configPath
		if saveErr := config.SaveConfig(configPath, unifiedConfig); saveErr != nil {
			log.Printf("警告: 无法创建默认配置文件 %s: %v（将在内存中使用默认配置）", configPath, saveErr)
		} else {
			log.Printf("已创建默认配置文件: %s，请通过配置 UI 填写凭据和认证设置", configPath)
			if cfg, absPath, loadErr := config.LoadConfig(configPath); loadErr == nil {
				unifiedConfig = cfg
				actualPath = absPath
			}
		}
	} else {
		log.Printf("加载配置文件: %s", actualPath)
	}
	finalPort = unifiedConfig.GetPort()

	// 非 daemon 子进程启动时，先检查 PID 文件，判断是否已有实例在运行
	if os.Getenv(envDaemonMode) != "1" {
		pidPath := filepath.Join("logs", adminPIDName)
		if data, err := os.ReadFile(pidPath); err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil {
				if proc, err := os.FindProcess(pid); err == nil {
					if proc.Signal(syscall.Signal(0)) == nil {
						fmt.Fprintf(os.Stderr, "sop-chat-server 已在运行（PID=%d），\n如需重启请先执行: ./sop-chat-server stop\n如需查看管理地址: ./sop-chat-server adminurl\n", pid)
						os.Exit(1)
					}
				}
			}
			// PID 文件存在但进程已不在，清理残留文件
			_ = os.Remove(pidPath)
		}
	}

	// 端口可用性检查
	// - 首次运行（无配置文件）：自动向后探测，避免因端口占用直接报错退出
	// - 已有配置文件：直接检测，占用则提示用户修改配置后退出（不自动切换）
	// daemon 子进程跳过此检查，直接在 net.Listen 时报错
	if os.Getenv(envDaemonMode) != "1" {
		if isFirstRun {
			if port, found := findAvailablePort(finalPort, 20); found {
				if port != finalPort {
					log.Printf("端口 %d 已被占用，自动切换到端口 %d", finalPort, port)
				}
				finalPort = port
			} else {
				log.Fatalf("无法在端口 %d~%d 范围内找到可用端口，请手动在 config.yaml 中指定端口", unifiedConfig.GetPort(), unifiedConfig.GetPort()+19)
			}
		} else {
			if !isPortAvailable(finalPort) {
				log.Fatalf("端口 %d 已被占用，请修改 %s 中的 global.port 配置后重试", finalPort, actualPath)
			}
		}
	}

	// daemon 子进程可能通过环境变量接收父进程已探测好的端口
	if p := os.Getenv(envAdminPort); p != "" && os.Getenv(envDaemonMode) == "1" {
		if port, err := strconv.Atoi(p); err == nil {
			finalPort = port
		}
	}

	listenAddr := fmt.Sprintf("%s:%d", unifiedConfig.GetHost(), finalPort)

	// ── Daemon 模式 ────────────────────────────────────────────────────────
	// 非 daemon 子进程 且 未指定 --no-daemon 时，将自身以 daemon 方式重启
	if os.Getenv(envDaemonMode) != "1" && !noDaemon {
		// 预生成 token，确保父进程打印的 URL 与 daemon 子进程使用的一致
		token, err := api.GenerateConfigUIToken()
		if err != nil {
			log.Fatalf("生成 admin token 失败: %v", err)
		}

		// 创建日志目录
		logsDir := "logs"
		if err := os.MkdirAll(logsDir, 0o755); err != nil {
			log.Fatalf("创建日志目录失败: %v", err)
		}
		logPath := filepath.Join(logsDir, adminLogName)
		// 必须用 *os.File 传给子进程，否则 exec 会建管道，父进程退出后管道断裂导致子进程 SIGPIPE
		logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			log.Fatalf("打开日志文件失败: %v", err)
		}

		// 构造 admin-ui URL 列表，写入文件（0600）并打印到终端
		adminURLs := buildAdminURLs(unifiedConfig.GetHost(), finalPort, token)
		urlPath := filepath.Join(logsDir, adminURLName)
		_ = os.WriteFile(urlPath, []byte(strings.Join(adminURLs, "\n")+"\n"), 0o600)

		// 向终端打印 admin-ui 地址
		printURLBox(adminURLs)
		fmt.Printf("日志输出: %s\n", logPath)

		// 以 daemon 方式重启自身
		exe, err := os.Executable()
		if err != nil {
			log.Fatalf("获取可执行文件路径失败: %v", err)
		}
		cmd := exec.Command(exe, os.Args[1:]...)
		cmd.Env = append(os.Environ(),
			envDaemonMode+"=1",
			envAdminToken+"="+token,
			fmt.Sprintf("%s=%d", envAdminPort, finalPort),
		)
		cmd.Stdout = logFile
		cmd.Stderr = logFile
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		if err := cmd.Start(); err != nil {
			log.Fatalf("启动守护进程失败: %v", err)
		}
		logFile.Close() // 父进程不再需要，关闭自己持有的 fd

		pidPath := filepath.Join(logsDir, adminPIDName)
		fmt.Printf("守护进程已启动 PID=%d，端口绑定成功后 PID 将写入 %s\n", cmd.Process.Pid, pidPath)
		fmt.Printf("停止服务: ./sop-chat-server stop\n")
		fmt.Printf("查看管理地址: ./sop-chat-server adminurl\n")

		return
	}
	// ── End Daemon ─────────────────────────────────────────────────────────

	// 加载客户端配置（凭据未配置时返回空 Config，不阻塞启动）
	clientConfig, _ := client.LoadConfig()

	// 启动 API 服务器（钉钉机器人的启动/停止由 server 内部管理）
	server, err := api.NewServer(clientConfig, unifiedConfig, actualPath)
	if err != nil {
		log.Fatalf("初始化服务器失败: %v", err)
	}

	// 打印配置 UI 访问链接（带 token，防止未授权访问）
	token := server.GetConfigUIToken()
	printConfigUI := func() {
		urls := buildAdminURLs(unifiedConfig.GetHost(), finalPort, token)
		log.Printf("╔══════════════════════════════════════════════════════════════╗")
		log.Printf("║  ⚙  配置管理 UI（仅本次启动有效，请勿分享此链接）            ║")
		for _, u := range urls {
			log.Printf("║  %s", u)
		}
		log.Printf("╚══════════════════════════════════════════════════════════════╝")
	}
	printConfigUI()

	isDaemon := os.Getenv(envDaemonMode) == "1"

	// 信号处理：
	// - daemon 模式：SIGTERM / SIGINT 均直接优雅退出（stop 命令依赖此行为）
	// - 前台模式：SIGTERM 直接退出；SIGINT（Ctrl+C）需 10s 内连按两次才退出
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

		var lastInt time.Time
		for sig := range sigCh {
			if isDaemon || sig == syscall.SIGTERM {
				log.Printf("收到退出信号，正在关闭服务...")
				os.Exit(0)
			}
			// 前台 SIGINT 双击确认
			now := time.Now()
			if !lastInt.IsZero() && now.Sub(lastInt) <= 10*time.Second {
				log.Printf("确认退出")
				os.Exit(0)
			}
			lastInt = now
			log.Printf("收到 Ctrl+C，10s 内再按一次确认退出")
			printConfigUI()
		}
	}()

	if isDaemon {
		// daemon 子进程内部接管日志输出，实现滚动（单文件 100 MB，保留 7 个归档，压缩）
		logsDir0 := "logs"
		_ = os.MkdirAll(logsDir0, 0o755)
		lj := &lumberjack.Logger{
			Filename:   filepath.Join(logsDir0, adminLogName),
			MaxSize:    100,
			MaxBackups: 7,
			Compress:   true,
		}
		log.SetOutput(lj)

		// daemon 子进程：先绑定端口，确认成功后再写 PID 文件
		l, err := net.Listen("tcp", listenAddr)
		if err != nil {
			log.Fatalf("监听端口 %s 失败: %v", listenAddr, err)
		}

		pidPath := filepath.Join(logsDir0, adminPIDName)
		if werr := os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o644); werr != nil {
			log.Printf("警告: 写入 PID 文件失败: %v", werr)
		} else {
			log.Printf("API 服务器已在 %s 上监听，PID=%d（%s）", listenAddr, os.Getpid(), pidPath)
		}

		if err := server.RunListener(l); err != nil {
			log.Fatalf("服务器退出: %v", err)
		}
	} else {
		log.Printf("启动 API 服务器，监听地址 %s", listenAddr)
		if err := server.Run(listenAddr); err != nil {
			log.Fatalf("服务器启动失败: %v", err)
		}
	}
}

// buildAdminURLs 根据 host、port、token 构造 admin-ui URL 列表。
func buildAdminURLs(host string, port int, token string) []string {
	var urls []string
	if host == "0.0.0.0" {
		for _, ip := range localAddresses() {
			urls = append(urls, fmt.Sprintf("http://%s:%d/admin-ui?token=%s", ip, port, token))
		}
	} else {
		urls = append(urls, fmt.Sprintf("http://%s:%d/admin-ui?token=%s", host, port, token))
	}
	return urls
}

// printURLBox 将 URL 列表以方框形式打印到标准输出。
func printURLBox(urls []string) {
	fmt.Println("╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║  ⚙  配置管理 UI（仅本次启动有效，请勿分享此链接）            ║")
	for _, u := range urls {
		fmt.Printf("║  %s\n", u)
	}
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")
}

// queryAdminURL 读取 URL 文件并打印 admin-ui 地址。
func queryAdminURL() {
	urlPath := filepath.Join("logs", adminURLName)
	data, err := os.ReadFile(urlPath)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "未找到管理地址文件 %s，服务可能未在运行\n", urlPath)
		} else {
			fmt.Fprintf(os.Stderr, "读取管理地址文件失败: %v\n", err)
		}
		os.Exit(1)
	}
	fmt.Println("╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║  ⚙  配置管理 UI                                              ║")
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line != "" {
			fmt.Printf("║  %s\n", line)
		}
	}
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")
}

// stopDaemon 读取 PID 文件并终止守护进程。
func stopDaemon() {
	pidPath := filepath.Join("logs", adminPIDName)
	data, err := os.ReadFile(pidPath)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "未找到 PID 文件 %s，服务可能未在运行\n", pidPath)
		} else {
			fmt.Fprintf(os.Stderr, "读取 PID 文件失败: %v\n", err)
		}
		os.Exit(1)
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		fmt.Fprintf(os.Stderr, "PID 文件内容无效: %v\n", err)
		os.Exit(1)
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		fmt.Fprintf(os.Stderr, "找不到进程 PID=%d: %v\n", pid, err)
		os.Exit(1)
	}

	if err := proc.Signal(syscall.SIGTERM); err != nil {
		fmt.Fprintf(os.Stderr, "向进程 PID=%d 发送信号失败: %v\n", pid, err)
		os.Exit(1)
	}
	fmt.Printf("已向进程 PID=%d 发送 SIGTERM，等待退出...\n", pid)

	// 持续轮询进程是否已退出，最多等待 8 秒
	deadline := time.Now().Add(8 * time.Second)
	cleanupFiles := func() {
		_ = os.Remove(pidPath)
		_ = os.Remove(filepath.Join("logs", adminURLName))
	}

	for time.Now().Before(deadline) {
		time.Sleep(200 * time.Millisecond)
		if err := proc.Signal(syscall.Signal(0)); err != nil {
			fmt.Printf("进程 PID=%d 已退出\n", pid)
			cleanupFiles()
			return
		}
	}

	// 超时后强制 SIGKILL
	fmt.Printf("进程 PID=%d 在 8s 内未退出，发送 SIGKILL 强制终止\n", pid)
	if err := proc.Signal(syscall.SIGKILL); err != nil {
		fmt.Fprintf(os.Stderr, "SIGKILL 发送失败: %v\n", err)
		os.Exit(1)
	}

	// SIGKILL 后再等待最多 3 秒确认进程已消失
	killDeadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(killDeadline) {
		time.Sleep(200 * time.Millisecond)
		if err := proc.Signal(syscall.Signal(0)); err != nil {
			fmt.Printf("进程 PID=%d 已强制终止\n", pid)
			cleanupFiles()
			return
		}
	}
	fmt.Fprintf(os.Stderr, "警告: 进程 PID=%d 在 SIGKILL 后仍未消失，请手动检查\n", pid)
}

// findAvailablePort 从 startPort 开始向后依次尝试最多 maxTries 个端口。
func findAvailablePort(startPort, maxTries int) (int, bool) {
	for i := 0; i < maxTries; i++ {
		port := startPort + i
		ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
		if err == nil {
			ln.Close()
			return port, true
		}
	}
	return 0, false
}

// isPortAvailable 检查指定端口是否可以被监听。
func isPortAvailable(port int) bool {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return false
	}
	ln.Close()
	return true
}

// localAddresses 返回本机所有真实 IPv4 单播地址，过滤掉常见虚拟网桥接口。
func localAddresses() []string {
	skipPrefixes := []string{"docker", "br-", "veth", "virbr", "vmnet", "vboxnet", "tun", "tap"}

	ifaces, err := net.Interfaces()
	if err != nil {
		return []string{"localhost"}
	}

	var addrs []string
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		name := strings.ToLower(iface.Name)
		skip := false
		for _, prefix := range skipPrefixes {
			if strings.HasPrefix(name, prefix) {
				skip = true
				break
			}
		}
		if skip {
			continue
		}

		ifAddrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range ifAddrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() || ip.To4() == nil {
				continue
			}
			addrs = append(addrs, ip.String())
		}
	}

	return append([]string{"localhost"}, addrs...)
}

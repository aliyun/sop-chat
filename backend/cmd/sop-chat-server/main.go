package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"sop-chat/internal/api"
	"sop-chat/internal/client"
	"sop-chat/internal/config"

	"github.com/joho/godotenv"
)

func main() {
	// 解析命令行参数
	var configPath string
	var showHelp bool
	flag.StringVar(&configPath, "config", "", "配置文件路径（默认: config.yaml 或 CONFIG_PATH 环境变量）")
	flag.StringVar(&configPath, "c", "", "配置文件路径（-config 的简写）")
	flag.BoolVar(&showHelp, "help", false, "显示帮助信息")
	flag.BoolVar(&showHelp, "h", false, "显示帮助信息")
	flag.Usage = func() {
		log.Printf("SOP Chat API Server\n\n")
		log.Printf("用法: %s [选项]\n\n", os.Args[0])
		log.Printf("选项:\n")
		flag.PrintDefaults()
		log.Printf("\n示例:\n")
		log.Printf("  %s -config /path/to/config.yaml\n", os.Args[0])
		log.Printf("  %s -c ./config.yaml\n", os.Args[0])
		log.Printf("  %s --config /etc/sop-chat/config.yaml\n", os.Args[0])
		log.Printf("\n注意: 端口配置请在 config.yaml 的 global.port 中设置\n")
	}
	flag.Parse()

	// 显示帮助信息
	if showHelp {
		flag.Usage()
		os.Exit(0)
	}

	// 首先加载当前目录的 .env 文件
	// 如果文件不存在，忽略错误（使用系统环境变量）
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
		// 如果通过命令行指定了配置文件路径，设置到环境变量中
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
			// 重新加载刚写入的文件以获取规范化的绝对路径
			if cfg, absPath, loadErr := config.LoadConfig(configPath); loadErr == nil {
				unifiedConfig = cfg
				actualPath = absPath
			}
		}
	} else {
		log.Printf("加载配置文件: %s", actualPath)
	}
	finalPort = unifiedConfig.GetPort()

	// 首次运行（无配置文件）时自动探测可用端口，避免因端口占用直接报错退出
	if isFirstRun {
		if port, found := findAvailablePort(finalPort, 20); found {
			if port != finalPort {
				log.Printf("端口 %d 已被占用，自动切换到端口 %d", finalPort, port)
			}
			finalPort = port
		} else {
			log.Fatalf("无法在端口 %d~%d 范围内找到可用端口，请手动在 config.yaml 中指定端口", unifiedConfig.GetPort(), unifiedConfig.GetPort()+19)
		}
	}

	listenAddr := fmt.Sprintf("%s:%d", unifiedConfig.GetHost(), finalPort)

	// 加载客户端配置（凭据未配置时返回空 Config，不阻塞启动）
	clientConfig, _ := client.LoadConfig()

	// 启动 API 服务器（钉钉机器人的启动/停止由 server 内部管理）
	server, err := api.NewServer(clientConfig, unifiedConfig, actualPath)
	if err != nil {
		log.Fatalf("初始化服务器失败: %v", err)
	}

	// 打印配置 UI 访问链接（带 token，防止未授权访问）
	token := server.GetConfigUIToken()
	// printConfigUI 打印配置管理 UI 访问链接
	printConfigUI := func() {
		log.Printf("╔══════════════════════════════════════════════════════════════╗")
		log.Printf("║  ⚙  配置管理 UI（仅本次启动有效，请勿分享此链接）            ║")
		if unifiedConfig.GetHost() == "0.0.0.0" {
			for _, ip := range localAddresses() {
				log.Printf("║  http://%s:%d/config-ui?token=%s", ip, finalPort, token)
			}
		} else {
			log.Printf("║  http://%s:%d/config-ui?token=%s", unifiedConfig.GetHost(), finalPort, token)
		}
		log.Printf("╚══════════════════════════════════════════════════════════════╝")
	}

	printConfigUI()

	// 捕获 SIGINT（Ctrl+C）：第一次提醒配置 UI 地址，第二次才真正退出
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

		<-sigCh
		log.Printf("收到退出信号，再按一次 Ctrl+C 确认退出")
		printConfigUI()

		<-sigCh
		log.Printf("确认退出")
		os.Exit(0)
	}()

	log.Printf("启动 API 服务器，监听地址 %s", listenAddr)
	if err := server.Run(listenAddr); err != nil {
		log.Fatalf("服务器启动失败: %v", err)
	}
}

// findAvailablePort 从 startPort 开始向后依次尝试最多 maxTries 个端口，
// 返回第一个可以成功 listen 的端口号。仅用于首次运行时的自动端口探测。
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

// localAddresses 返回本机所有真实 IPv4 单播地址，过滤掉常见 Docker / 虚拟网桥接口。
// 过滤规则：接口名前缀匹配 docker、br-、veth、virbr、vmnet、vboxnet、tun、tap。
func localAddresses() []string {
	skipPrefixes := []string{"docker", "br-", "veth", "virbr", "vmnet", "vboxnet", "tun", "tap"}

	ifaces, err := net.Interfaces()
	if err != nil {
		return []string{"localhost"}
	}

	var addrs []string
	for _, iface := range ifaces {
		// 跳过 down 状态和 loopback（loopback 单独追加 localhost）
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
			// 只保留 IPv4 单播地址
			if ip == nil || ip.IsLoopback() || ip.To4() == nil {
				continue
			}
			addrs = append(addrs, ip.String())
		}
	}

	// 始终把 localhost 放在最前面，方便本地访问
	return append([]string{"localhost"}, addrs...)
}

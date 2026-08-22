// elec 宿舍电费监控 — 单二进制管理工具。
//
// 用法:
//   elec                     # 启动服务（webapp + 采集）(等同于 run)
//   elec run                 # 启动服务（webapp + 采集循环）
//   elec install             # 安装到 /opt/elec/ 并注册 systemd 服务
//   elec collect             # 单次采集
//   elec status              # 查看服务状态
//   elec logs                # 查看实时日志
//   elec update              # 检查并更新到最新版本
//   elec token               # 查看 token 状态
//   elec config              # 查看配置路径
//   elec help                # 显示帮助
//
// 环境变量:
//   ELEc_DIR    数据目录（默认 /opt/elec）
//   ADMIN_KEY   管理密钥（自动生成）
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/mico-v/wxxyshall-monitoring/internal/auth"
	"github.com/mico-v/wxxyshall-monitoring/internal/charge"
	"github.com/mico-v/wxxyshall-monitoring/internal/config"
	"github.com/mico-v/wxxyshall-monitoring/internal/db"
	"github.com/mico-v/wxxyshall-monitoring/internal/rate"
	"github.com/mico-v/wxxyshall-monitoring/internal/web"
)

const (
	installDir = "/opt/elec"
	serviceName = "elec"
)

func main() {
	// 设置数据目录环境变量，使 config 包使用正确的路径
	os.Setenv("USTS_DATA_DIR", dataDir())

	// 解析子命令
	args := os.Args[1:]
	cmd := "run"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd = args[0]
		args = args[1:]
	}

	switch cmd {
	case "install":
		cmdInstall()
	case "run":
		cmdRun()
	case "collect":
		cmdCollect()
	case "status":
		cmdStatus()
	case "logs":
		cmdLogs()
	case "update":
		cmdUpdate()
	case "token":
		cmdToken()
	case "config":
		cmdConfig()
	case "help", "--help", "-h":
		printHelp()
	default:
		fmt.Printf("未知命令: %s\n\n", cmd)
		printHelp()
		os.Exit(1)
	}
}

func printHelp() {
	fmt.Println(`宿舍电费监控 — 管理工具

用法:
  elec                    启动服务（webapp + 采集循环）
  elec run                启动服务
  elec install            安装到 /opt/elec/ 并注册 systemd 服务
  elec collect            单次采集
  elec status             查看服务状态
  elec logs               查看实时日志
  elec update             检查并更新到最新版本
  elec token              查看 token 状态
  elec config             查看配置信息
  elec help               显示帮助

环境变量:
  ELEc_DIR    数据目录（默认 /opt/elec）
  ADMIN_KEY   管理密钥（自动生成，查看: elec config）

首次使用:
  1. sudo ./elec install     # 安装到系统
  2. 浏览器打开 http://服务器IP:8080
  3. 在「查询设置」中添加要监控的宿舍
  4. 本地运行 python3 login.py --push http://服务器IP:8080
`)
}

// elecDir 返回数据目录。
func elecDir() string {
	if d := os.Getenv("ELEc_DIR"); d != "" {
		return d
	}
	return installDir
}

// dataDir 返回数据子目录。
func dataDir() string {
	return filepath.Join(elecDir(), "data")
}

// -------- 安装 --------

func cmdInstall() {
	if os.Geteuid() != 0 {
		fmt.Println("安装需要 root 权限，请使用: sudo elec install")
		os.Exit(1)
	}

	dir := elecDir()
	data := dataDir()

	// 确保目录存在
	os.MkdirAll(data, 0755)

	// 复制自身到安装目录
	self, _ := os.Executable()
	target := filepath.Join(dir, "elec")
	if self != target {
		copyFile(self, target)
		os.Chmod(target, 0755)
		fmt.Printf("[✓] 已复制到 %s\n", target)
	}

	// 生成默认配置
	config.GenerateDefaultConfig()

	// 生成 ADMIN_KEY
	keyFile := filepath.Join(data, ".admin_key")
	adminKey := ""
	if data, err := os.ReadFile(keyFile); err == nil {
		adminKey = strings.TrimSpace(string(data))
	} else {
		adminKey = generateKey()
		os.WriteFile(keyFile, []byte(adminKey+"\n"), 0600)
	}

	// 写入 systemd 服务
	svc := fmt.Sprintf(`[Unit]
Description=宿舍电费监控
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=%s
Environment="ELEc_DIR=%s"
Environment="ADMIN_KEY=%s"
Environment="TZ=Asia/Shanghai"
ExecStart=%s/elec run
Restart=always
RestartSec=30
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
`, dir, dir, adminKey, dir)

	svcPath := "/etc/systemd/system/" + serviceName + ".service"
	if err := os.WriteFile(svcPath, []byte(svc), 0644); err != nil {
		fmt.Printf("[✗] 写入 systemd 服务失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("[✓] systemd 服务已创建: %s\n", svcPath)

	// 创建定时采集 timer
	timerSvc := fmt.Sprintf(`[Unit]
Description=宿舍电费监控 - 定时采集
After=network.target

[Service]
Type=oneshot
User=root
WorkingDirectory=%s
Environment="ELEc_DIR=%s"
ExecStart=%s/elec collect
`, dir, dir, dir)

	timerUnit := fmt.Sprintf(`[Unit]
Description=宿舍电费监控 - 定时采集（每 60 分钟）
Requires=%s-collect.service

[Timer]
OnBootSec=2min
OnUnitActiveSec=60min
Unit=%s-collect.service

[Install]
WantedBy=timers.target
`, serviceName, serviceName)

	os.WriteFile("/etc/systemd/system/"+serviceName+"-collect.service", []byte(timerSvc), 0644)
	os.WriteFile("/etc/systemd/system/"+serviceName+"-collect.timer", []byte(timerUnit), 0644)

	// 重载并启用
	exec.Command("systemctl", "daemon-reload").Run()
	exec.Command("systemctl", "enable", serviceName).Run()
	exec.Command("systemctl", "enable", serviceName+"-collect.timer").Run()
	exec.Command("systemctl", "start", serviceName+"-collect.timer").Run()

	fmt.Println("")
	fmt.Println("==============================================")
	fmt.Println("  安装完成!")
	fmt.Println("==============================================")
	fmt.Println("")
	fmt.Printf("  启动服务:   systemctl start %s\n", serviceName)
	fmt.Printf("  查看状态:   systemctl status %s\n", serviceName)
	fmt.Printf("  查看日志:   journalctl -u %s -f\n", serviceName)
	fmt.Println("")
	fmt.Printf("  配置文件:   %s/config.json\n", data)
	fmt.Printf("  管理密钥:   %s\n", adminKey)
	fmt.Println("")
	fmt.Println("  仪表盘:     http://服务器IP:8080")
	fmt.Println("")
	fmt.Println("  登录（约 70 天一次）:")
	fmt.Printf("  ADMIN_KEY=%s python3 login.py --push http://服务器IP:8080\n", adminKey)
	fmt.Println("")
}

// -------- 运行 --------

func cmdRun() {
	dir := elecDir()
	data := dataDir()

	// 确保 data 目录存在
	os.MkdirAll(data, 0755)

	// 生成默认配置
	config.GenerateDefaultConfig()

	// 读取配置
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("配置加载失败: %v", err)
	}

	adminKey := os.Getenv("ADMIN_KEY")
	if adminKey == "" {
		// 尝试从 .admin_key 读取
		keyData, _ := os.ReadFile(filepath.Join(data, ".admin_key"))
		adminKey = strings.TrimSpace(string(keyData))
	}

	// 启动采集循环（后台 goroutine）
	go func() {
		runCollectLoop(cfg)
	}()

	// 启动 webapp
	server, err := web.NewServer(
		filepath.Join(data, "config.json"),
		filepath.Join(data, "token.json"),
		filepath.Join(data, "electricity.db"),
		adminKey,
		dir,
	)
	if err != nil {
		log.Fatalf("服务器初始化失败: %v", err)
	}

	port := cfg.Port
	if port <= 0 {
		port = 8080
	}

	addr := fmt.Sprintf(":%d", port)
	httpServer := &http.Server{
		Addr:         addr,
		Handler:      server.Handler(),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 0,
		IdleTimeout:  60 * time.Second,
	}

	// 优雅关闭
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		log.Printf("仪表盘启动: http://0.0.0.0:%d (Ctrl+C 退出)", port)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP 服务器错误: %v", err)
		}
	}()

	<-sigCh
	log.Println("收到退出信号，正在关闭服务器...")
	server.CloseSSE()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(ctx); err != nil {
		log.Printf("服务器已关闭（部分 SSE 连接被强制断开: %v）", err)
	}
	log.Println("服务器已关闭")
}

// runCollectLoop 后台采集循环。
func runCollectLoop(cfg *config.Config) {
	// 等待网络就绪
	time.Sleep(5 * time.Second)

	interval := time.Duration(cfg.PollIntervalMin) * time.Minute
	if interval <= 0 {
		interval = 60 * time.Minute
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// 立即执行一次
	doCollect(cfg)

	for range ticker.C {
		doCollect(cfg)
	}
}

func doCollect(cfg *config.Config) {
	tok, err := config.LoadToken()
	if err != nil || tok == nil || tok.AccessToken == "" {
		log.Printf("采集跳过: 未找到 token")
		return
	}
	if auth.IsExpired(tok, 600) {
		log.Printf("采集跳过: token 已过期，请重新登录")
		return
	}

	targets := cfg.GetTargets()
	if len(targets) == 0 {
		return
	}

	database, err := db.Open(filepath.Join(dataDir(), "electricity.db"))
	if err != nil {
		log.Printf("采集失败: 打开数据库: %v", err)
		return
	}
	defer database.Close()

	database.BackfillRoomIDs(cfg)
	base := cfg.BaseURL
	clients := make(map[int]*charge.Client)
	limiter := rate.NewLimiter(cfg.RateLimitPerMinute)

	for _, t := range targets {
		if cfg.RateLimitPerMinute > 0 && !limiter.Allow() {
			log.Printf("采集限流，跳过剩余宿舍")
			break
		}

		client, ok := clients[t.FeeItemID]
		if !ok {
			client = charge.NewClient(base, tok.AccessToken)
			if err := client.Establish(t.FeeItemID, t.AppID); err != nil {
				if charge.IsAuthError(err) {
					log.Printf("登录态已失效，请重新登录")
					return
				}
				log.Printf("建立会话失败(%s): %v", t.DisplayLabel(), err)
				continue
			}
			clients[t.FeeItemID] = client
		}

		reading, err := client.QueryBalance(t.FeeItemID, t.Campus, t.Building, t.Room)
		if err != nil {
			if charge.IsAuthError(err) {
				log.Printf("登录态已失效，请重新登录")
				return
			}
			log.Printf("查询失败(%s): %v", t.DisplayLabel(), err)
			continue
		}

		database.InsertReading(t, struct {
			SurplusCharge *float64
			Show          map[string]string
			Raw           map[string]any
		}{
			SurplusCharge: reading.SurplusCharge,
			Show:          reading.Show,
			Raw:           reading.Raw,
		})
	}
}

// -------- 采集 --------

func cmdCollect() {
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("配置加载失败: %v", err)
	}
	doCollect(cfg)
}

// -------- 状态/日志 --------

func cmdStatus() {
	cmd := exec.Command("systemctl", "status", serviceName, "--no-pager", "-l")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Run()
}

func cmdLogs() {
	cmd := exec.Command("journalctl", "-u", serviceName, "-n", "50", "--no-pager", "-f")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Run()
}

// -------- 更新 --------

func cmdUpdate() {
	repo := "mico-v/wxxyshall_monitoring"
	api := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)

	resp, err := http.Get(api)
	if err != nil {
		fmt.Printf("[✗] 检查更新失败: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	var release struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}
	json.NewDecoder(resp.Body).Decode(&release)

	if release.TagName == "" {
		fmt.Println("[✗] 无法获取最新版本信息")
		os.Exit(1)
	}

	// 查找当前架构的包
	arch := runtime.GOARCH
	if arch == "aarch64" {
		arch = "arm64"
	}
	suffix := fmt.Sprintf("linux-%s.tar.gz", arch)
	var downloadURL string
	for _, a := range release.Assets {
		if strings.Contains(a.Name, suffix) {
			downloadURL = a.BrowserDownloadURL
			break
		}
	}

	if downloadURL == "" {
		fmt.Printf("[✗] 未找到 %s 架构的更新包\n", suffix)
		os.Exit(1)
	}

	fmt.Printf("[*] 发现新版本: %s\n", release.TagName)
	fmt.Printf("[*] 下载: %s\n", downloadURL)

	// 下载
	resp, err = http.Get(downloadURL)
	if err != nil {
		fmt.Printf("[✗] 下载失败: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	// 解压到临时目录
	tmpDir, _ := os.MkdirTemp("", "elec-update")
	defer os.RemoveAll(tmpDir)

	cmd := exec.Command("tar", "xzf", "-")
	cmd.Dir = tmpDir
	cmd.Stdin = resp.Body
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Printf("[✗] 解压失败: %v\n", err)
		os.Exit(1)
	}

	// 替换二进制
	dir := elecDir()
	os.MkdirAll(dir, 0755)
	copyFile(filepath.Join(tmpDir, "elec"), filepath.Join(dir, "elec"))
	os.Chmod(filepath.Join(dir, "elec"), 0755)

	fmt.Printf("[✓] 已更新到 %s\n", release.TagName)
	fmt.Println("[*] 重启服务: systemctl restart elec")
}

// -------- Token/Config --------

func cmdToken() {
	tok, err := config.LoadToken()
	if err != nil {
		log.Fatalf("读取 token 失败: %v", err)
	}
	if tok == nil || tok.AccessToken == "" {
		fmt.Println("未找到 token.json，请先运行 python3 login.py")
		return
	}

	fmt.Printf("学号(sno)      : %s\n", tok.Sno)
	fmt.Printf("来源(source)   : %s\n", tok.Source)
	if tok.LoginTime > 0 {
		fmt.Printf("获取时间       : %s\n", time.Unix(tok.LoginTime, 0).Format("2006-01-02 15:04:05"))
	}

	left := auth.SecondsLeft(tok)
	if left == nil {
		fmt.Println("剩余有效期     : 未知")
	} else {
		fmt.Printf("剩余有效期     : %.1f 天\n", *left/86400)
	}

	if auth.IsExpired(tok, 600) {
		fmt.Println("状态           : 已过期，请重新运行 python3 login.py")
	}
}

func cmdConfig() {
	dir := elecDir()
	data := dataDir()

	fmt.Printf("安装目录: %s\n", dir)
	fmt.Printf("数据目录: %s\n", data)
	fmt.Printf("配置文件: %s/config.json\n", data)
	fmt.Printf("Token 文件: %s/token.json\n", data)
	fmt.Printf("数据库:   %s/electricity.db\n", data)

	keyFile := filepath.Join(data, ".admin_key")
	if keyData, err := os.ReadFile(keyFile); err == nil {
		fmt.Printf("管理密钥: %s\n", strings.TrimSpace(string(keyData)))
	}

	if cfg, err := config.LoadConfig(); err == nil {
		fmt.Println("")
		fmt.Printf("端口:     %d\n", cfg.Port)
		fmt.Printf("学号:     %s\n", cfg.Username)
		fmt.Printf("宿舍数:   %d\n", len(cfg.GetTargets()))
		fmt.Printf("采集间隔: %d 分钟\n", cfg.PollIntervalMin)
		fmt.Printf("限流:     %d 次/分钟\n", cfg.RateLimitPerMinute)
	}
}

// -------- 辅助 --------

func generateKey() string {
	b := make([]byte, 24)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func copyFile(src, dst string) error {
	s, err := os.Open(src)
	if err != nil {
		return err
	}
	defer s.Close()

	d, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer d.Close()

	_, err = io.Copy(d, s)
	return err
}
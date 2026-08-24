// elec 宿舍电费监控 — 单二进制管理工具。
//
// 用法:
//
//	elec                     # 启动服务（webapp + 采集）(等同于 run)
//	elec run                 # 启动服务（webapp + 采集循环）
//	elec install             # 安装到 /opt/elec/ 并注册 systemd 服务
//	elec collect             # 单次采集
//	elec status              # 查看服务状态
//	elec logs                # 查看实时日志
//	elec token               # 查看 token 状态
//	elec config              # 查看配置路径
//	elec help                # 显示帮助
//
// 环境变量:
//
//	ELEc_DIR    数据目录（默认 /opt/elec）
//	ADMIN_KEY   管理密钥（自动生成）
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/mico-v/wxxyshall-monitoring/internal/auth"
	"github.com/mico-v/wxxyshall-monitoring/internal/collector"
	"github.com/mico-v/wxxyshall-monitoring/internal/config"
	"github.com/mico-v/wxxyshall-monitoring/internal/db"
	"github.com/mico-v/wxxyshall-monitoring/internal/web"
)

const (
	installDir  = "/opt/elec"
	serviceName = "elec"
)

func main() {
	// 初始化结构化日志
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

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
	fmt.Print(`宿舍电费监控 — 管理工具

用法:
  elec                    启动服务（webapp + 采集循环）
  elec run                启动服务
  elec install            安装到 /opt/elec/ 并注册 systemd 服务
  elec collect            单次采集
  elec status             查看服务状态
  elec logs               查看实时日志
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
	if err := validateInstallPath(dir); err != nil {
		fatalInstall("安装目录无效", err)
	}

	if err := os.MkdirAll(dir, 0755); err != nil {
		fatalInstall("创建安装目录失败", err)
	}
	if err := os.MkdirAll(data, 0750); err != nil {
		fatalInstall("创建数据目录失败", err)
	}
	if err := validateInstallPath(data); err != nil {
		fatalInstall("数据目录无效", err)
	}
	if err := secureDataPermissions(data); err != nil {
		fatalInstall("数据目录安全检查失败", err)
	}
	serviceUser, uid, gid, err := ensureServiceUser(dir)
	if err != nil {
		fatalInstall("创建服务用户失败", err)
	}

	// 复制自身到安装目录
	self, err := os.Executable()
	if err != nil {
		fatalInstall("获取当前二进制路径失败", err)
	}
	target := filepath.Join(dir, "elec")
	if self != target {
		if err := copyFile(self, target); err != nil {
			fatalInstall("复制二进制失败", err)
		}
		fmt.Printf("[✓] 已复制到 %s\n", target)
	}
	if err := os.Chmod(target, 0755); err != nil {
		fatalInstall("设置二进制权限失败", err)
	}

	// 生成默认配置
	if err := config.GenerateDefaultConfig(); err != nil {
		fatalInstall("生成默认配置失败", err)
	}
	dashboardPort := config.DefaultPort
	if cfg, err := config.LoadConfig(); err == nil {
		dashboardPort = cfg.Port
	}

	// 生成 ADMIN_KEY
	keyFile := filepath.Join(data, ".admin_key")
	if _, err := loadOrCreateAdminKey(data); err != nil {
		fatalInstall("生成管理密钥失败", err)
	}
	if err := secureDataPermissions(data); err != nil {
		fatalInstall("收紧数据目录权限失败", err)
	}
	if err := filepath.WalkDir(data, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		return os.Chown(path, uid, gid)
	}); err != nil {
		fatalInstall("设置数据目录所有者失败", err)
	}

	// 写入 systemd 服务
	svc := fmt.Sprintf(`[Unit]
Description=宿舍电费监控
After=network.target

[Service]
Type=simple
User=%s
Group=%s
WorkingDirectory=%s
Environment="ELEc_DIR=%s"
Environment="TZ=Asia/Shanghai"
ExecStart=%s/elec run
Restart=always
RestartSec=30
UMask=0027
NoNewPrivileges=true
PrivateTmp=true
PrivateDevices=true
ProtectSystem=strict
ReadWritePaths=%s
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectControlGroups=true
ProtectClock=true
RestrictSUIDSGID=true
RestrictRealtime=true
LockPersonality=true
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6
CapabilityBoundingSet=
AmbientCapabilities=
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
`, serviceUser, serviceUser, dir, dir, dir, data)

	svcPath := "/etc/systemd/system/" + serviceName + ".service"
	if err := writeFileAtomic(svcPath, []byte(svc), 0644); err != nil {
		fmt.Printf("[✗] 写入 systemd 服务失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("[✓] systemd 服务已创建: %s\n", svcPath)

	// 只生成并启用主服务(采集间隔由 config.json 的 poll_interval_minutes 控制,
	// 不再创建固定的 60 分钟 systemd timer,避免与配置不一致)

	// 重载并启用
	for _, args := range [][]string{
		{"daemon-reload"},
		{"enable", serviceName},
		{"restart", serviceName},
	} {
		if output, err := exec.Command("systemctl", args...).CombinedOutput(); err != nil {
			fatalInstall("systemctl "+strings.Join(args, " ")+" 失败", fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output))))
		}
	}

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
	fmt.Printf("  管理密钥:   %s（仅本机可读）\n", keyFile)
	fmt.Println("")
	fmt.Printf("  仪表盘:     http://服务器IP:%d\n", dashboardPort)
	fmt.Println("")
	fmt.Println("  登录（约 70 天一次）:")
	fmt.Printf("  先安全读取密钥: sudo cat %s\n", keyFile)
	fmt.Printf("  再在本地执行: ADMIN_KEY='<密钥>' python3 login.py --push http://服务器IP:%d --push-only\n", dashboardPort)
	fmt.Println("")
}

// -------- 运行 --------

func cmdRun() {
	dir := elecDir()
	data := dataDir()

	if err := os.MkdirAll(data, 0750); err != nil {
		slog.Error("创建数据目录失败", "err", err)
		os.Exit(1)
	}
	if err := rejectSymlinkComponents(data); err != nil {
		slog.Error("数据目录无效", "err", err)
		os.Exit(1)
	}
	if err := secureDataPermissions(data); err != nil {
		slog.Error("数据目录安全检查失败", "err", err)
		os.Exit(1)
	}

	// 生成默认配置
	if err := config.GenerateDefaultConfig(); err != nil {
		slog.Error("生成默认配置失败", "err", err)
		os.Exit(1)
	}
	if err := secureDataPermissions(data); err != nil {
		slog.Error("收紧数据目录权限失败", "err", err)
		os.Exit(1)
	}

	// 配置/Token 热重载 Hub(文件被修改后自动生效,无需重启)
	hub, err := config.NewHub()
	if err != nil {
		slog.Error("配置加载失败", "err", err)
		os.Exit(1)
	}

	adminKey := strings.TrimSpace(os.Getenv("ADMIN_KEY"))
	if adminKey == "" {
		adminKey, err = loadOrCreateAdminKey(data)
		if err != nil {
			slog.Error("加载管理密钥失败", "err", err)
			os.Exit(1)
		}
	}
	if len(adminKey) < 16 || len(adminKey) > 256 {
		slog.Error("管理密钥长度必须在 16..256 个字符之间")
		os.Exit(1)
	}

	// 启动 webapp
	server, err := web.NewServer(
		hub,
		filepath.Join(data, "electricity.db"),
		adminKey,
		dir,
	)
	if err != nil {
		slog.Error("服务器初始化失败", "err", err)
		os.Exit(1)
	}
	runCtx, stopRun := context.WithCancel(context.Background())
	defer stopRun()
	go runCollectLoop(runCtx, hub, server.Collector())

	port := hub.Config().Port
	if port <= 0 {
		port = 8080
	}

	addr := fmt.Sprintf(":%d", port)
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           server.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second, // SSE handler 会在每次推送前单独刷新写截止时间。
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    32 << 10,
	}

	// 优雅关闭
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		slog.Info("仪表盘启动", "addr", addr, "port", port)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("HTTP 服务器错误", "err", err)
			os.Exit(1)
		}
	}()

	<-sigCh
	slog.Info("收到退出信号，正在关闭服务器")
	stopRun()
	server.CloseSSE()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(ctx); err != nil {
		slog.Warn("服务器已关闭（部分 SSE 连接被强制断开）", "err", err)
	}
	if err := server.Close(); err != nil {
		slog.Warn("关闭服务资源不完整", "err", err)
	}
	slog.Info("服务器已关闭")
}

// runCollectLoop 后台采集循环:
//   - 按配置 poll_interval_minutes 定时采集;
//   - 每 2 秒检测一次 config.json / token.json 变更,配置/间隔/token 修改自动生效;
//   - token 临近过期时每天提醒一次。
func runCollectLoop(ctx context.Context, hub *config.Hub, service *collector.Service) {
	select {
	case <-time.After(5 * time.Second):
	case <-ctx.Done():
		return
	}

	initialCfg := hub.Config()
	interval := collectInterval(initialCfg)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// 立即执行一次
	go doCollect(ctx, hub.Config(), service)

	watch := time.NewTicker(2 * time.Second)
	defer watch.Stop()

	var lastExpiryWarn time.Time

	for {
		select {
		case <-ticker.C:
			go doCollect(ctx, hub.Config(), service)
		case <-watch.C:
			cfgChanged, tokChanged, err := hub.Reload()
			if err != nil {
				slog.Warn("配置热重载失败(将保留上次配置)", "err", err)
			}
			if tokChanged {
				slog.Info("检测到 token.json 变更，已热重载")
			}
			cfg := hub.Config()
			if service.Limiter().Rate() != cfg.RateLimitPerMinute {
				service.SetRate(cfg.RateLimitPerMinute)
				slog.Info("请求间隔已更新", "rate_limit_per_minute", cfg.RateLimitPerMinute,
					"request_interval", service.Limiter().Interval())
			}
			if cfgChanged {
				slog.Info("检测到 config.json 变更，已热重载",
					"targets", len(cfg.GetTargets()),
					"interval_minutes", cfg.PollIntervalMin,
					"rate_limit", cfg.RateLimitPerMinute)
			}

			// 间隔变了 → 重建 ticker
			ni := collectInterval(cfg)
			if ni != interval {
				ticker.Reset(ni)
				interval = ni
				slog.Info("采集间隔已更新", "interval_minutes", int(ni/time.Minute))
			}
			warnTokenExpiry(hub.Token(), &lastExpiryWarn)
		case <-ctx.Done():
			return
		}
	}
}

// collectInterval 从配置取采集间隔,非法值回退 60 分钟。
func collectInterval(cfg *config.Config) time.Duration {
	if cfg == nil || cfg.PollIntervalMin <= 0 {
		return 60 * time.Minute
	}
	return time.Duration(cfg.PollIntervalMin) * time.Minute
}

// warnTokenExpiry 在 token 临近过期(剩余 < 7 天)时每天提醒一次。
func warnTokenExpiry(tok *config.Token, last *time.Time) {
	if tok == nil || last == nil {
		return
	}
	left := auth.DaysLeft(tok)
	if left == math.MaxInt64 || left > 7 {
		return
	}
	if time.Since(*last) < 24*time.Hour {
		return
	}
	*last = time.Now()
	slog.Warn("token 即将过期，请重新运行 login.py --push", "days_left", left)
}

func doCollect(ctx context.Context, cfg *config.Config, service *collector.Service) {
	if cfg == nil || service == nil {
		return
	}
	targets := cfg.GetTargets()
	if len(targets) == 0 {
		return
	}
	results, err := service.CollectAll(ctx, targets, nil)
	if errors.Is(err, collector.ErrBusy) {
		slog.Info("定时采集跳过: 已有采集任务运行中")
		return
	}
	if err != nil && !errors.Is(err, context.Canceled) {
		slog.Warn("定时采集提前结束", "err", err)
	}
	for _, result := range results {
		if result.Err != nil {
			slog.Warn("采集失败", "room", result.Target.DisplayLabel(), "err", result.Err)
		}
	}
}

// -------- 采集 --------

func cmdCollect() {
	if err := runCollectCommand(); err != nil {
		slog.Error("单次采集失败", "err", err)
		os.Exit(1)
	}
}

func runCollectCommand() error {
	hub, err := config.NewHub()
	if err != nil {
		return fmt.Errorf("配置加载失败: %w", err)
	}
	database, err := db.Open(config.DBPath())
	if err != nil {
		return fmt.Errorf("打开数据库失败: %w", err)
	}
	defer database.Close()
	targets := hub.Config().GetTargets()
	if len(targets) == 0 {
		return fmt.Errorf("没有配置监控宿舍")
	}
	results, err := collector.New(hub, database).CollectAll(context.Background(), targets, nil)
	if err != nil {
		return err
	}
	failed := 0
	for _, result := range results {
		if result.Err != nil {
			failed++
			slog.Warn("采集失败", "room", result.Target.DisplayLabel(), "err", result.Err)
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d/%d 个宿舍采集失败", failed, len(targets))
	}
	slog.Info("单次采集完成", "success", len(results))
	return nil
}

// -------- 状态/日志 --------

func cmdStatus() {
	cmd := exec.Command("systemctl", "status", serviceName, "--no-pager", "-l")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		os.Exit(1)
	}
}

func cmdLogs() {
	cmd := exec.Command("journalctl", "-u", serviceName, "-n", "50", "--no-pager", "-f")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		os.Exit(1)
	}
}

// -------- Token/Config --------

func cmdToken() {
	tok, err := config.LoadToken()
	if err != nil {
		slog.Error("读取 token 失败", "err", err)
		os.Exit(1)
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
	fmt.Printf("管理密钥: %s（内容不直接输出）\n", keyFile)

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

func generateKey() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func copyFile(src, dst string) error {
	s, err := os.Open(src)
	if err != nil {
		return err
	}
	defer s.Close()

	dir := filepath.Dir(dst)
	d, err := os.CreateTemp(dir, ".elec-install-*")
	if err != nil {
		return err
	}
	tmp := d.Name()
	ok := false
	defer func() {
		_ = d.Close()
		if !ok {
			_ = os.Remove(tmp)
		}
	}()

	if err := d.Chmod(0755); err != nil {
		return err
	}
	if _, err := io.Copy(d, s); err != nil {
		return err
	}
	if err := d.Sync(); err != nil {
		return err
	}
	if err := d.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		return err
	}
	if dirFile, err := os.Open(dir); err == nil {
		_ = dirFile.Sync()
		_ = dirFile.Close()
	}
	ok = true
	return nil
}

func ensureServiceUser(home string) (string, int, int, error) {
	const name = "elec"
	usr, err := user.Lookup(name)
	if err != nil {
		if _, ok := err.(user.UnknownUserError); !ok {
			return "", 0, 0, err
		}
		output, createErr := exec.Command(
			"useradd", "--system", "--user-group", "--home-dir", home, "--shell", nologinShell(), name,
		).CombinedOutput()
		if createErr != nil {
			return "", 0, 0, fmt.Errorf("%w: %s", createErr, strings.TrimSpace(string(output)))
		}
		usr, err = user.Lookup(name)
		if err != nil {
			return "", 0, 0, err
		}
	}
	if filepath.Clean(usr.HomeDir) != filepath.Clean(home) {
		return "", 0, 0, fmt.Errorf("现有用户 %s 的主目录是 %s，不是专用安装目录 %s", name, usr.HomeDir, home)
	}
	group, err := user.LookupGroup(name)
	if err != nil {
		return "", 0, 0, fmt.Errorf("缺少同名专用用户组 %s: %w", name, err)
	}
	if group.Gid != usr.Gid {
		return "", 0, 0, fmt.Errorf("用户 %s 的主组 GID %s 与同名组 GID %s 不一致", name, usr.Gid, group.Gid)
	}
	uid, err := strconv.Atoi(usr.Uid)
	if err != nil {
		return "", 0, 0, fmt.Errorf("无效 UID %q: %w", usr.Uid, err)
	}
	gid, err := strconv.Atoi(usr.Gid)
	if err != nil {
		return "", 0, 0, fmt.Errorf("无效 GID %q: %w", usr.Gid, err)
	}
	if uid == 0 || gid == 0 {
		return "", 0, 0, fmt.Errorf("拒绝使用 root 身份的 %s 用户/组", name)
	}
	return name, uid, gid, nil
}

func nologinShell() string {
	for _, path := range []string{"/usr/sbin/nologin", "/sbin/nologin"} {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path
		}
	}
	return "/bin/false"
}

func validateInstallPath(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return fmt.Errorf("必须是规范化绝对路径")
	}
	if path == string(filepath.Separator) || filepath.Dir(path) == string(filepath.Separator) {
		return fmt.Errorf("路径范围过大，至少需要两级目录")
	}
	if strings.ContainsAny(path, " \t\r\n\"'%") || strings.Contains(path, "\\") {
		return fmt.Errorf("不能包含空白、反斜杠、引号、百分号或单引号")
	}
	return rejectSymlinkComponents(path)
}

func rejectSymlinkComponents(path string) error {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("解析绝对路径失败: %w", err)
	}
	current := string(filepath.Separator)
	for _, part := range strings.Split(strings.TrimPrefix(absPath, string(filepath.Separator)), string(filepath.Separator)) {
		if part == "" {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("检查路径组件 %s 失败: %w", current, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("路径组件不能是符号链接: %s", current)
		}
	}
	return nil
}

func loadOrCreateAdminKey(data string) (string, error) {
	keyFile := filepath.Join(data, ".admin_key")
	keyData, err := os.ReadFile(keyFile)
	if err == nil {
		key := strings.TrimSpace(string(keyData))
		if len(key) < 16 || len(key) > 256 {
			return "", fmt.Errorf("%s 中的管理密钥长度必须在 16..256 个字符之间", keyFile)
		}
		if err := os.Chmod(keyFile, 0600); err != nil {
			return "", err
		}
		return key, nil
	}
	if !os.IsNotExist(err) {
		return "", err
	}
	key, err := generateKey()
	if err != nil {
		return "", err
	}
	f, err := os.OpenFile(keyFile, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if os.IsExist(err) {
		return loadOrCreateAdminKey(data)
	}
	if err != nil {
		return "", err
	}
	if _, err := f.WriteString(key + "\n"); err != nil {
		_ = f.Close()
		_ = os.Remove(keyFile)
		return "", err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(keyFile)
		return "", err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(keyFile)
		return "", err
	}
	if dirFile, err := os.Open(data); err == nil {
		_ = dirFile.Sync()
		_ = dirFile.Close()
	}
	return key, nil
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".elec-write-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	ok := false
	defer func() {
		_ = f.Close()
		if !ok {
			_ = os.Remove(tmp)
		}
	}()
	if err := f.Chmod(mode); err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	if dirFile, err := os.Open(dir); err == nil {
		_ = dirFile.Sync()
		_ = dirFile.Close()
	}
	ok = true
	return nil
}

func secureDataPermissions(data string) error {
	return filepath.WalkDir(data, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("数据目录不允许符号链接: %s", path)
		}
		if entry.IsDir() {
			return os.Chmod(path, 0750)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("数据目录只允许普通文件: %s", path)
		}
		mode := os.FileMode(0640)
		if entry.Name() == ".admin_key" || entry.Name() == "token.json" {
			mode = 0600
		}
		return os.Chmod(path, mode)
	})
}

func fatalInstall(message string, err error) {
	fmt.Printf("[✗] %s: %v\n", message, err)
	os.Exit(1)
}

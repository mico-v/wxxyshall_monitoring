// webapp HTTP 仪表盘服务器。
//
// 用法：
//   webapp [--host 0.0.0.0] [--port 8080] [--config-dir /app/data]
package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mico-v/wxxyshall-monitoring/internal/config"
	"github.com/mico-v/wxxyshall-monitoring/internal/web"
)

func main() {
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("配置加载失败: %v", err)
	}

	host := flag.String("host", "127.0.0.1", "监听地址")
	port := flag.String("port", "", "监听端口（默认从 config.json 的 port 字段读取，仍为空则 8080）")
	flag.Parse()

	adminKey := os.Getenv("ADMIN_KEY")
	if adminKey == "" {
		adminKey = cfg.AdminKey
	}
	rootDir := config.DataDir()

	server, err := web.NewServer(
		config.ConfigPath(),
		config.TokenPath(),
		config.DBPath(),
		adminKey,
		rootDir,
	)
	if err != nil {
		log.Fatalf("服务器初始化失败: %v", err)
	}

	// 端口优先级: 命令行 > config.json > 8080
	listenPort := *port
	if listenPort == "" {
		if cfg.Port > 0 {
			listenPort = fmt.Sprintf("%d", cfg.Port)
		} else {
			listenPort = "8080"
		}
	}
	addr := *host + ":" + listenPort
	httpServer := &http.Server{
		Addr:         addr,
		Handler:      server.Handler(),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 0, // SSE 需要无限 write timeout
		IdleTimeout:  60 * time.Second,
	}

	// 优雅关闭
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		log.Printf("仪表盘启动: http://%s (Ctrl+C 退出)", addr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP 服务器错误: %v", err)
		}
	}()

	<-sigCh
	log.Println("收到退出信号，正在关闭服务器...")

	// 先关闭 SSE Hub（断开所有 SSE 客户端连接）
	server.CloseSSE()

	// 给服务器一点时间完成正在处理的请求，但允许 SSE 连接被强制断开
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(ctx); err != nil {
		// SSE 连接是长连接，Shutdown 超时是预期行为，不 panic
		log.Printf("服务器已关闭（部分 SSE 连接被强制断开: %v）", err)
	}

	log.Println("服务器已关闭")
}
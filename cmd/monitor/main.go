// monitor 采集守护进程。
//
// 用法：
//   monitor                    # 查一次所有监控宿舍并入库
//   monitor --loop             # 常驻，按配置 poll_interval_minutes 循环
//   monitor --config-dir /app/data  # 指定数据目录
package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mico-v/wxxyshall-monitoring/internal/auth"
	"github.com/mico-v/wxxyshall-monitoring/internal/charge"
	"github.com/mico-v/wxxyshall-monitoring/internal/config"
	"github.com/mico-v/wxxyshall-monitoring/internal/db"
)

func main() {
	loop := flag.Bool("loop", false, "循环模式")
	flag.Parse()

	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("配置加载失败: %v", err)
	}

	tok, err := config.LoadToken()
	if err != nil {
		log.Fatalf("Token 加载失败: %v", err)
	}
	if tok == nil || tok.AccessToken == "" {
		log.Fatalf("未找到有效 token.json，请先运行: python login.py")
	}

	if *loop {
		interval := time.Duration(cfg.PollIntervalMin) * time.Minute
		log.Printf("循环监控 %d 个宿舍，每 %s 一次。Ctrl+C 退出。", len(cfg.GetTargets()), interval)
		runLoop(cfg, tok, interval)
	} else {
		ok := runOnce(cfg, tok)
		if !ok {
			os.Exit(1)
		}
	}
}

// runLoop 循环采集模式。
func runLoop(cfg *config.Config, tok *config.Token, interval time.Duration) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// 立即执行一次
	ok := runOnce(cfg, tok)
	nextSleep := interval
	if !ok {
		nextSleep = minDuration(interval, 5*time.Minute)
	}

	for {
		select {
		case <-ticker.C:
			ok := runOnce(cfg, tok)
			if !ok {
				nextSleep = minDuration(interval, 5*time.Minute)
				ticker.Reset(nextSleep)
			} else {
				ticker.Reset(interval)
			}
		case <-sigCh:
			log.Println("收到退出信号，停止采集。")
			return
		}
	}
}

// runOnce 执行一次全部采集。
// 返回 true 表示至少一个宿舍采集成功。
func runOnce(cfg *config.Config, tok *config.Token) bool {
	targets := cfg.GetTargets()
	if len(targets) == 0 {
		log.Println("config.json 里没有 targets，请先用网页查询设置添加要监控的宿舍。")
		return false
	}

	if auth.IsExpired(tok, 600) {
		log.Println("token 已过期或临近过期（续期不可用），请重新运行: python login.py")
		return false
	}

	// 打开数据库
	database, err := db.Open(config.DBPath())
	if err != nil {
		log.Printf("打开数据库失败: %v", err)
		return false
	}
	defer database.Close()

	// 回填旧数据
	if err := database.BackfillRoomIDs(cfg); err != nil {
		log.Printf("回填宿舍 ID 失败: %v", err)
	}

	base := cfg.BaseURL
	anyOK := false

	// 按 (feeitemid, appId) 分组复用 ChargeClient
	type clientKey struct {
		feeitemid int
		appID     int
	}
	clients := make(map[clientKey]*charge.Client)

	log.Printf("开始采集 %d 个宿舍", len(targets))
	for _, t := range targets {
		key := clientKey{feeitemid: t.FeeItemID, appID: t.AppID}
		client, ok := clients[key]
		if !ok {
			client = charge.NewClient(base, tok.AccessToken)
			if err := client.Establish(t.FeeItemID, t.AppID); err != nil {
				if charge.IsAuthError(err) {
					log.Printf("登录态已失效，请重新运行: python login.py")
					return false
				}
				log.Printf("建立会话失败(%s %s/%s/%s): %v", t.DisplayLabel(), t.Campus, t.Building, t.Room, err)
				continue
			}
			clients[key] = client
		}

		reading, err := client.QueryBalance(t.FeeItemID, t.Campus, t.Building, t.Room)
		if err != nil {
			if charge.IsAuthError(err) {
				log.Printf("登录态已失效，请重新运行: python login.py")
				return false
			}
			log.Printf("查询失败(%s): %v", t.DisplayLabel(), err)
			continue
		}

		// 入库
		insertData := struct {
			SurplusCharge *float64
			Show          map[string]string
			Raw           map[string]any
		}{
			SurplusCharge: reading.SurplusCharge,
			Show:          reading.Show,
			Raw:           reading.Raw,
		}
		if err := database.InsertReading(t, insertData); err != nil {
			log.Printf("入库失败(%s): %v", t.DisplayLabel(), err)
			continue
		}
		anyOK = true
	}

	return anyOK
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
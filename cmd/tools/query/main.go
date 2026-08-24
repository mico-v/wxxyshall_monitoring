// query 直接查询任意房间的电费。
//
// 用法: query <campus> <building> <room>
package main

import (
	"fmt"
	"log"
	"os"

	"github.com/mico-v/wxxyshall-monitoring/internal/charge"
	"github.com/mico-v/wxxyshall-monitoring/internal/config"
	"github.com/mico-v/wxxyshall-monitoring/internal/rate"
)

func main() {
	if len(os.Args) < 4 {
		log.Fatalf("用法: query <campus> <building> <room>")
	}

	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("配置加载失败: %v", err)
	}
	tok, err := config.LoadToken()
	if err != nil {
		log.Fatalf("Token 加载失败: %v", err)
	}
	if tok == nil || tok.AccessToken == "" {
		log.Fatalf("未找到 token.json，请先运行 python login.py")
	}

	targets := cfg.GetTargets()
	if len(targets) == 0 {
		log.Fatalf("config.json 没有 targets")
	}

	campus, building, room := os.Args[1], os.Args[2], os.Args[3]
	fid := targets[0].FeeItemID

	client := charge.NewClientWithLimiter(cfg.BaseURL, tok.AccessToken, rate.NewLimiter(cfg.RateLimitPerMinute))
	if err := client.Establish(fid, targets[0].AppID); err != nil {
		log.Fatalf("建立会话失败: %v", err)
	}

	reading, err := client.QueryBalance(fid, campus, building, room)
	if err != nil {
		log.Fatalf("查询失败: %v", err)
	}

	for k, v := range reading.Show {
		fmt.Printf("  %s = %s\n", k, v)
	}
}

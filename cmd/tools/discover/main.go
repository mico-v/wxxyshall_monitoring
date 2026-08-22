// discover 列出校区/楼栋/房间的取值。
//
// 用法:
//   discover                    # 列校区
//   discover <campus>           # 列该校区楼栋
//   discover <campus> <building> # 列该楼栋房间
package main

import (
	"fmt"
	"log"
	"os"

	"github.com/mico-v/wxxyshall-monitoring/internal/charge"
	"github.com/mico-v/wxxyshall-monitoring/internal/config"
)

func main() {
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
		log.Fatalf("config.json 没有 targets，请先添加要监控的宿舍")
	}

	fid := targets[0].FeeItemID
	client := charge.NewClient(cfg.BaseURL, tok.AccessToken)
	if err := client.Establish(fid, targets[0].AppID); err != nil {
		log.Fatalf("建立会话失败: %v", err)
	}

	args := os.Args[1:]
	switch len(args) {
	case 0:
		campuses, err := client.ListCampuses(fid)
		if err != nil {
			log.Fatalf("获取校区列表失败: %v", err)
		}
		fmt.Println("校区:")
		for _, c := range campuses {
			fmt.Printf("  campus=%-6s  %s\n", c.Value, c.Name)
		}
	case 1:
		buildings, err := client.ListBuildings(fid, args[0])
		if err != nil {
			log.Fatalf("获取楼栋列表失败: %v", err)
		}
		fmt.Printf("校区 %s 的楼栋:\n", args[0])
		for _, b := range buildings {
			fmt.Printf("  building=%-8s  %s\n", b.Value, b.Name)
		}
	default:
		rooms, err := client.ListRooms(fid, args[0], args[1])
		if err != nil {
			log.Fatalf("获取房间列表失败: %v", err)
		}
		fmt.Printf("校区 %s 楼栋 %s 的房间:\n", args[0], args[1])
		for _, r := range rooms {
			fmt.Printf("  room=%-8s  %s\n", r.Value, r.Name)
		}
	}
}
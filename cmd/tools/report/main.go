// report 查看电费历史记录。
//
// 用法: report [N条]
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"

	"github.com/mico-v/wxxyshall-monitoring/internal/config"
	"github.com/mico-v/wxxyshall-monitoring/internal/db"
)

func main() {
	flag.Parse()

	n := 50
	if flag.NArg() > 0 {
		if v, err := strconv.Atoi(flag.Arg(0)); err == nil && v > 0 && v <= 10000 {
			n = v
		} else {
			log.Fatalf("N 必须是 1..10000 的整数")
		}
	}

	database, err := db.Open(config.DBPath())
	if err != nil {
		log.Fatalf("打开数据库失败: %v", err)
	}
	defer database.Close()

	rows, err := database.QueryReadings(0, "", "", "")
	if err != nil {
		log.Fatalf("查询失败: %v", err)
	}

	if len(rows) == 0 {
		fmt.Println("暂无记录，请先运行 elec collect 或启动 elec run")
		os.Exit(0)
	}

	// 显示最近 N 条（新->旧）
	start := 0
	if len(rows) > n {
		start = len(rows) - n
	}
	display := rows[start:]
	deltas := make([]string, len(display))
	previous := make(map[string]float64)
	for i, row := range display {
		key := row.Campus + "|" + row.Building + "|" + row.Room
		if row.SurplusCharge == nil {
			continue
		}
		if prev, ok := previous[key]; ok {
			delta := *row.SurplusCharge - prev
			if delta > 0.01 || delta < -0.01 {
				deltas[i] = fmt.Sprintf("  (剩余变化 %+.2f)", delta)
			}
		}
		previous[key] = *row.SurplusCharge
	}

	fmt.Printf("最近 %d 条（新->旧）:\n", len(display))
	for i := len(display) - 1; i >= 0; i-- {
		row := display[i]
		showStr := ""
		for k, v := range row.Show {
			showStr += fmt.Sprintf("  %s=%s", k, v)
		}
		fmt.Printf("%s  %s%s%s\n", row.TS, row.RoomLabel, showStr, deltas[i])
	}
}

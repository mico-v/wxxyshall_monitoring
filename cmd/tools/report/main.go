// report 查看电费历史记录。
//
// 用法: report [--config-dir /app/data] [N条]
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
		if v, err := strconv.Atoi(flag.Arg(0)); err == nil {
			n = v
		}
	}

	database, err := db.Open(config.DBPath())
	if err != nil {
		log.Fatalf("打开数据库失败: %v", err)
	}
	defer database.Close()

	rows, err := database.QueryReadings(n, "", "", "")
	if err != nil {
		log.Fatalf("查询失败: %v", err)
	}

	if len(rows) == 0 {
		fmt.Println("暂无记录，先运行 monitor")
		os.Exit(0)
	}

	// 显示最近 N 条（新->旧）
	start := 0
	if len(rows) > n {
		start = len(rows) - n
	}
	display := rows[start:]

	fmt.Printf("最近 %d 条（新->旧）:\n", len(display))
	var prev *float64
	for _, row := range display {
		delta := ""
		if prev != nil && row.SurplusCharge != nil {
			d := *row.SurplusCharge - *prev
			if d > 0.01 || d < -0.01 {
				delta = fmt.Sprintf("  (剩余变化 %+.2f)", d)
			}
		}
		prev = row.SurplusCharge

		showStr := ""
		for k, v := range row.Show {
			showStr += fmt.Sprintf("  %s=%s", k, v)
		}
		fmt.Printf("%s  %s%s%s\n", row.TS, row.RoomLabel, showStr, delta)
	}
}
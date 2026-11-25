// token_status 查看 token 状态：有效期、是否临近过期。
//
// 用法: token_status（数据目录由 ELEC_DIR 指定）
package main

import (
	"fmt"
	"log"
	"time"

	"github.com/mico-v/wxxyshall-monitoring/internal/auth"
	"github.com/mico-v/wxxyshall-monitoring/internal/config"
)

func main() {
	tok, err := config.LoadToken()
	if err != nil {
		log.Fatalf("读取 token 失败: %v", err)
	}
	if tok == nil || tok.AccessToken == "" {
		log.Fatalf("未找到 token.json，请在网页「查询设置 → 学校账号」登录")
	}

	fmt.Printf("学号(sno)      : %s\n", tok.Sno)
	fmt.Printf("来源(source)   : %s\n", tok.Source)
	if tok.LoginTime > 0 {
		fmt.Printf("获取时间       : %s\n", time.Unix(tok.LoginTime, 0).Format("2006-01-02 15:04:05"))
	}

	left := auth.SecondsLeft(tok)
	if left == nil {
		fmt.Println("剩余有效期     : 未知（缺 expires_in/login_time 元数据）")
	} else {
		exp := time.Unix(*auth.ExpiryEpoch(tok), 0)
		fmt.Printf("过期时间       : %s（约 %.1f 天后）\n", exp.Format("2006-01-02 15:04:05"), *left/86400)
	}

	fmt.Println("续期           : 服务端续期不可用，到期需重新登录学校账号")

	if left != nil && auth.IsExpired(tok, 600) {
		fmt.Println("状态           : 已过期或临近过期，请在网页中重新登录学校账号。")
	}
}

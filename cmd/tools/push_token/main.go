// push_token 把本地 token.json 推送到服务器 webapp 的 /api/token。
//
// 用法:
//   USTS_ADMIN_KEY=<key> push_token <服务器URL>
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/mico-v/wxxyshall-monitoring/internal/config"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatalf("用法: USTS_ADMIN_KEY=<key> push_token <服务器URL>")
	}

	serverURL := strings.TrimRight(os.Args[1], "/")
	apiURL := serverURL + "/api/token"

	key := os.Getenv("USTS_ADMIN_KEY")
	if key == "" {
		key = os.Getenv("ADMIN_KEY")
	}
	if key == "" {
		log.Fatalf("缺少 USTS_ADMIN_KEY 环境变量（与服务器 ADMIN_KEY 一致）")
	}

	tok, err := config.LoadToken()
	if err != nil {
		log.Fatalf("读取 token 失败: %v", err)
	}
	if tok == nil || tok.AccessToken == "" {
		log.Fatalf("token.json 里没有 access_token，请先运行 python login.py")
	}

	body, _ := json.Marshal(tok)
	req, err := http.NewRequest("POST", apiURL, bytes.NewReader(body))
	if err != nil {
		log.Fatalf("创建请求失败: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Fatalf("推送失败: %v", err)
	}
	defer resp.Body.Close()

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)

	if resp.StatusCode != 200 {
		errMsg := "unknown"
		if result != nil {
			errMsg, _ = result["error"].(string)
		}
		log.Fatalf("推送失败 HTTP %d: %s", resp.StatusCode, errMsg)
	}

	days := 0
	if result != nil {
		if d, ok := result["expires_in_days"].(float64); ok {
			days = int(d)
		}
	}
	fmt.Printf("[OK] token 已推送到 %s。有效期约 %d 天。\n", apiURL, days)
}
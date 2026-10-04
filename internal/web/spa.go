package web

import (
	"bytes"
	"embed"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

// dist 是 Vite 前端构建产物（frontend/ 构建输出到 internal/web/dist）。
// 提交该目录可保证 `go build` / `go test` 在没有任何 Node 工具链时也能工作；
// CI 会重新构建并校验产物是否与源码同步。
//
//go:embed all:dist
var distEmbedded embed.FS

var distFS fs.FS

func init() {
	sub, err := fs.Sub(distEmbedded, "dist")
	if err != nil {
		slog.Error("初始化前端产物文件系统失败", "err", err)
		panic(err)
	}
	distFS = sub
}

// readDistFile 读取前端构建产物中的文件。
func readDistFile(name string) ([]byte, error) {
	return fs.ReadFile(distFS, name)
}

// handleSPAAsset 提供 Vite 打包出的 /assets/*（文件名含内容 hash，可长期缓存）。
func (s *Server) handleSPAAsset(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/assets/")
	if name == "" || strings.Contains(name, "..") {
		s.handle404(w, r)
		return
	}
	data, err := readDistFile(path.Join("assets", name))
	if err != nil {
		s.handle404(w, r)
		return
	}
	ctype := mime.TypeByExtension(path.Ext(name))
	if ctype == "" {
		ctype = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	_, _ = w.Write(data)
}

// serveSPAIndex 输出前端 index.html，并注入构建版本与首屏主页可见性。
func (s *Server) serveSPAIndex(w http.ResponseWriter, homepageShown bool) {
	data, err := readDistFile("index.html")
	if err != nil {
		// 回退到磁盘（本地开发时直接替换 dist 目录的场景）。
		data, err = os.ReadFile(filepath.Join(s.rootDir, "dist", "index.html"))
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
	}
	value := "true"
	if !homepageShown {
		value = "false"
	}
	data = injectVersion(data)
	data = bytes.Replace(
		data,
		[]byte(`<body data-show-homepage="true">`),
		[]byte(`<body data-show-homepage="`+value+`">`),
		1,
	)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(data)
}

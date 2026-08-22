package web

import (
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"strings"
)

//go:embed static
var embeddedFiles embed.FS

// staticFS 是嵌入文件系统的子文件系统（static/ 目录）。
var staticFS fs.FS

func init() {
	var err error
	staticFS, err = fs.Sub(embeddedFiles, "static")
	if err != nil {
		slog.Error("初始化嵌入文件系统失败", "err", err)
		panic(err)
	}
}

// embeddedFileServer 返回一个 HTTP 处理器，从嵌入的文件系统提供静态文件服务。
func (s *Server) embeddedFileServer() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 去掉 /static/ 前缀
		filePath := strings.TrimPrefix(r.URL.Path, "/static/")
		filePath = strings.TrimLeft(filePath, "/")

		if filePath == "" {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}

		data, err := fs.ReadFile(staticFS, filePath)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}

		// 内容类型
		ext := path.Ext(filePath)
		contentTypes := map[string]string{
			".js":    "application/javascript; charset=utf-8",
			".css":   "text/css; charset=utf-8",
			".json":  "application/json; charset=utf-8",
			".svg":   "image/svg+xml",
			".png":   "image/png",
			".woff2": "font/woff2",
			".html":  "text/html; charset=utf-8",
			".ico":   "image/x-icon",
		}
		ctype := contentTypes[ext]
		if ctype == "" {
			ctype = "application/octet-stream"
		}

		w.Header().Set("Content-Type", ctype)
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
		w.Header().Set("Cache-Control", "public, max-age=3600")
		w.WriteHeader(http.StatusOK)
		w.Write(data)
	})
}

// readEmbeddedFile 读取嵌入文件系统中的文件。
func readEmbeddedFile(name string) ([]byte, error) {
	return fs.ReadFile(staticFS, name)
}

// embeddedFileExists 检查嵌入文件系统中是否存在文件。
func embeddedFileExists(name string) bool {
	_, err := fs.Stat(staticFS, name)
	return err == nil
}
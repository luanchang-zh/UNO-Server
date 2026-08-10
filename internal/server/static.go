package server

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// registerStatic 在 WebDir 存在时挂载前端静态页面。
//
// 静态托管仅作为 API 与 WebSocket 之外的兜底路由：
// 未命中的路径统一回退 index.html，交由前端单页应用自行路由。
func registerStatic(mux *http.ServeMux, webDir string) bool {
	if webDir == "" {
		return false
	}
	indexPath := filepath.Join(webDir, "index.html")
	if _, err := os.Stat(indexPath); err != nil {
		return false
	}
	fileServer := http.FileServer(http.Dir(webDir))
	mux.HandleFunc("/", func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet && request.Method != http.MethodHead {
			http.NotFound(writer, request)
			return
		}
		// 带扩展名的真实文件走文件服务，其余路径回退单页入口。
		requestPath := strings.TrimPrefix(request.URL.Path, "/")
		if requestPath != "" && !strings.HasSuffix(requestPath, "/") {
			fullPath := filepath.Join(webDir, filepath.FromSlash(requestPath))
			if info, err := os.Stat(fullPath); err == nil && !info.IsDir() {
				fileServer.ServeHTTP(writer, request)
				return
			}
		}
		http.ServeFile(writer, request, indexPath)
	})
	return true
}

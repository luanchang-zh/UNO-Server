import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// 开发期把 API 与 WebSocket 代理到本地 Go 服务，生产构建产物由 Go 静态托管。
export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      '/api': { target: 'http://localhost:8080', changeOrigin: true },
      '/ws': { target: 'ws://localhost:8080', ws: true },
    },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
  },
})

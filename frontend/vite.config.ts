import path from 'node:path';
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import tailwindcss from '@tailwindcss/vite';

// 构建产物直接落到 Go 的 embed 目录，供 internal/web 内嵌进单二进制。
export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: { '@': path.resolve(import.meta.dirname, 'src') },
  },
  server: {
    // 显式监听所有网卡（IPv4+IPv6）。Vite 默认的 localhost 在部分 Node/系统上只绑定
    // 到 [::1]，导致用 127.0.0.1 或局域网 IP 访问时连接被拒。
    host: true,
    port: 5173,
    strictPort: true,
    // 本地开发时把接口与 PWA 静态资源代理到 Go 服务（默认 127.0.0.1:5009）。
    proxy: Object.fromEntries(
      ['/api', '/static', '/sw.js', '/manifest.json', '/favicon.ico', '/404.html', '/offline.html'].map(
        (prefix) => [prefix, { target: process.env.ELEC_DEV_TARGET || 'http://127.0.0.1:5009' }],
      ),
    ),
  },
  build: {
    outDir: path.resolve(import.meta.dirname, '../internal/web/dist'),
    emptyOutDir: true,
    assetsDir: 'assets',
    sourcemap: false,
    target: 'es2022',
  },
});

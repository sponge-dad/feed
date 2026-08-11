import { fileURLToPath, URL } from 'node:url';
import { defineConfig, loadEnv } from 'vite';
import react from '@vitejs/plugin-react';

// 开发环境通过 dev server 代理 /api/v1 -> 网关，避免前端处理 CORS
export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), '');
  // 网关地址仅在本地开发代理使用，可通过 .env 的 VITE_GATEWAY_TARGET 覆盖
  const target = env.VITE_GATEWAY_TARGET || 'http://127.0.0.1:8080';
  return {
    plugins: [react()],
    resolve: {
      alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) },
    },
    server: {
      host: true, // 监听 0.0.0.0，允许通过 CVM 地址（如 OA/公网）直接访问
      port: 5173,
      proxy: {
        '/api/v1': {
          target,
          changeOrigin: true,
        },
      },
    },
  };
});

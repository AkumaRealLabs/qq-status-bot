import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import path from 'node:path'

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  // 分包由 AdminApp 里各 feature 页面的 React.lazy() 驱动：入口 chunk 只装
  // 外壳与鉴权，切标签页才拉对应 chunk。
  // 此处避免激进 manualChunks — Vite 8/Rolldown 下易形成循环 chunk 图。
})

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
  // 公开/管理分包由 React.lazy() 入口驱动：
  // PublicStatusPage / shared 不进 AdminStatusPage 及其他 admin 标签。
  // 此处避免激进 manualChunks — Vite 8/Rolldown 下易形成循环 chunk 图，
  // 并可能让公开页下载仅 admin 依赖（如 @dnd-kit）。
})

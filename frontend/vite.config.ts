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
  // Public vs admin separation is driven by React.lazy() entry points:
  // PublicStatusPage / shared stay out of AdminStatusPage + other admin tabs.
  // Avoid aggressive manualChunks here — they create circular chunk graphs in Vite 8/Rolldown
  // and can force the public page to download admin-only deps (e.g. @dnd-kit).
})

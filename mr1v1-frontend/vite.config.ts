import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      '/api/manager': 'http://localhost:8181',
    },
  },
  build: {
    outDir: '../mr1v1-server/internal/backend/static',
    emptyOutDir: true,
  },
})

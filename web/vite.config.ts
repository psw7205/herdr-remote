import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import { serviceWorkerBuildId } from './plugins/swBuildId'

export default defineConfig({
  plugins: [react(), serviceWorkerBuildId()],
  server: {
    proxy: { '/api': { target: 'http://127.0.0.1:8787', changeOrigin: true, ws: true } },
  },
})

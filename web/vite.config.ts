import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import { buildIdStamp } from './plugins/buildId'

export default defineConfig({
  plugins: [react(), buildIdStamp()],
  server: {
    proxy: { '/api': { target: 'http://127.0.0.1:8787', changeOrigin: true, ws: true } },
  },
})

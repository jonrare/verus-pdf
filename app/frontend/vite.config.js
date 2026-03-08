import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// Wails dev server always runs on localhost:34115 (configurable via wails.json "devServer").
// /wails/ipc.js and /wails/runtime.js are served from there, not from Vite.
// This proxy makes Vite forward those requests to Wails transparently.
export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      '/wails': 'http://localhost:34115',
    }
  }
})

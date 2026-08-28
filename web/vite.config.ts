import path from 'node:path'

import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

// The SPA is built into internal/webui/assets and embedded in olrd
// (design.md §6.3). Nothing here runs at runtime: the daemon serves static
// files, so there is no Node and no server-side rendering on the router.
export default defineConfig({
  plugins: [react(), tailwindcss()],

  resolve: {
    alias: { '@': path.resolve(import.meta.dirname, './src') },
  },

  build: {
    // Straight into the Go package that embeds it. `make web` restores the
    // .gitkeep afterwards, because emptyOutDir clears the directory.
    outDir: '../internal/webui/assets',
    emptyOutDir: true,
  },

  server: {
    // Development only. In production the SPA and the API share an origin, so
    // there is no proxy — which is also why the API ships no CORS headers.
    proxy: {
      '/api': {
        target: 'http://127.0.0.1:8080',
        changeOrigin: true,
      },
    },
  },
})

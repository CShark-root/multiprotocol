import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import { fileURLToPath, URL } from 'node:url'

// Production builds use relative asset URLs; the Go server injects
// <base href="/<secret>/"> so one build works under any secret path.
// The dev server mirrors `panel -dev`, which serves under /dev/.
export default defineConfig(({ command }) => ({
  base: command === 'serve' ? '/dev/' : './',
  plugins: [react(), tailwindcss()],
  resolve: { alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) } },
  server: {
    port: 5173,
    proxy: { '/dev/api': 'http://127.0.0.1:8080' },
  },
  // One ~160 kB (gzip) bundle is fine for an admin panel loaded once.
  build: { outDir: 'dist', emptyOutDir: true, chunkSizeWarningLimit: 800 },
}))

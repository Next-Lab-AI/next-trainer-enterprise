import vue from "@vitejs/plugin-vue"
import { defineConfig } from "vite"

export default defineConfig({
  plugins: [vue()],
  server: {
    port: Number(process.env.VITE_PORT || 5173),
    strictPort: false,
    host: "127.0.0.1",
    proxy: {
      "/api": process.env.VITE_API_TARGET || "http://127.0.0.1:8080",
      "/datasets": process.env.VITE_API_TARGET || "http://127.0.0.1:8080",
      "/artifacts": process.env.VITE_API_TARGET || "http://127.0.0.1:8080",
      "/agent": {
        target: process.env.VITE_API_TARGET || "http://127.0.0.1:8080",
        ws: true,
      },
    },
  },
  build: { outDir: "dist", emptyOutDir: true },
})

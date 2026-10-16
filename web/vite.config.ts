import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// RN_API — адрес rendol-api для разработки, например https://10.0.0.1
const api = process.env.RN_API ?? "http://127.0.0.1:8443";

export default defineConfig({
  plugins: [react()],
  build: { outDir: "dist", sourcemap: false, chunkSizeWarningLimit: 800 },
  server: {
    proxy: {
      "/v1": { target: api, changeOrigin: true, secure: false },
      "/healthz": { target: api, changeOrigin: true, secure: false },
    },
  },
});

import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// Dashboard is embedded into the collector binary from dist/. Dev proxies the
// API to a locally running collector on :8844.
export default defineConfig({
  plugins: [react()],
  build: { outDir: "dist", emptyOutDir: true },
  server: {
    proxy: {
      "/v1": "http://127.0.0.1:8844",
    },
  },
});

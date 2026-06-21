import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// During dev, proxy API + tracking calls to the Go server on :8080.
export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      "/api": "http://localhost:8080",
      "/t": "http://localhost:8080",
    },
  },
});

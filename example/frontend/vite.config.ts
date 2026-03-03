import { defineConfig } from "vite";

export default defineConfig({
  server: {
    port: 7002,
    proxy: {
      "/api": "http://localhost:7001",
    },
  },
});

import { defineConfig } from "vite";

export default defineConfig({
  root: "frontend",
  server: {
    port: 5173,
    strictPort: true,
    watch: { ignored: ["**/.mygo/**", "**/build/**"] },
  },
  build: {
    outDir: "../dist",
    emptyOutDir: true,
  },
});

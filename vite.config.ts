import { fileURLToPath } from "node:url";
import { defineConfig } from "vite";

// Every page of the app is an HTML entry: the main window, the region
// selection overlay and the pinned-image window.
const page = (file: string) =>
  fileURLToPath(new URL(`./frontend/${file}`, import.meta.url));

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
    rollupOptions: {
      input: {
        index: page("index.html"),
        overlay: page("overlay.html"),
        pin: page("pin.html"),
        scroll: page("scroll.html"),
      },
    },
  },
});

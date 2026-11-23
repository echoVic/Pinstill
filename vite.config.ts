import { fileURLToPath } from "node:url";
import { defineConfig } from "vite";

// Every page of the app is an HTML entry: the main window, the region
// selection overlay, the pinned-image window, the scrolling-capture control bar
// and the scrolling-capture result preview.
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
        pins: page("pins.html"),
        scroll: page("scroll.html"),
        frame: page("frame.html"),
        result: page("result.html"),
        line: page("line.html"),
      },
    },
  },
});

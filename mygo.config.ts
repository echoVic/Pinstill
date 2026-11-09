import { defineConfig } from "mygo-cli";

export default defineConfig({
  name: "Tupen",
  slug: "tupen",
  identifier: "dev.echovic.tupen",
  version: "0.1.0",
  publisher: "echovic",
  license: "MIT",
  main: "./cmd/tupen",
  bindings: "frontend/src/mygo.ts",
  devUrl: "http://localhost:5173",
  devCommand: "npm run dev:web",
  buildCommand: "npm run build:web",
  frontendDist: "dist",
  out: "build",
});

import { defineConfig } from "mygo-cli";

export default defineConfig({
  name: "Pinstill",
  slug: "pinstill",
  identifier: "dev.echovic.pinstill",
  version: "0.1.0",
  publisher: "echovic",
  license: "MIT",
  main: "./cmd/pinstill",
  bindings: "frontend/src/mygo.ts",
  devUrl: "http://localhost:5173",
  devCommand: "npm run dev:web",
  buildCommand: "npm run build:web",
  frontendDist: "dist",
  out: "build",

  // Ad-hoc signing by default so a fresh clone builds without a certificate.
  // A stable Developer ID signature keeps macOS TCC grants (screen recording,
  // accessibility) across rebuilds, so for day-to-day use build with:
  //   MACOS_SIGNING_IDENTITY="Developer ID Application: Your Name (TEAMID)" npm run build
  macos: {
    signingIdentity: process.env.MACOS_SIGNING_IDENTITY ?? "-",
  },
});

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

  // Release signing: `mygo build` signs the app and the dmg with this
  // Developer ID (hardened runtime + timestamp) and notarizes them, so macOS
  // keeps the screen recording / accessibility grants across rebuilds.
  // Override the identity with MACOS_SIGNING_IDENTITY="-" for an ad-hoc build
  // without a certificate. The notary profile comes from
  // `xcrun notarytool store-credentials`.
  macos: {
    minimumSystemVersion: "13.0",
    signingIdentity:
      process.env.MACOS_SIGNING_IDENTITY ?? "Developer ID Application: Zijie Hu (8LH7PFQJ89)",
    notarize: { keychainProfile: "clipnest-notary" },
  },
});

import { defineConfig } from "mygo-cli";

const signingIdentity = process.env.MACOS_SIGNING_IDENTITY?.trim() || "-";
const notaryProfile = process.env.MACOS_NOTARY_PROFILE?.trim();

export default defineConfig({
  name: "Pinstill",
  slug: "pinstill",
  identifier: "dev.echovic.pinstill",
  version: "0.1.3",
  publisher: "echovic",
  license: "MIT",
  main: "./cmd/pinstill",
  bindings: "frontend/src/mygo.ts",
  devUrl: "http://localhost:5173",
  devCommand: "npm run dev:web",
  buildCommand: "npm run build:web",
  frontendDist: "dist",
  out: "build",
  updates: {
    github: "echoVic/Pinstill",
    publicKey: "oO0mIN2AOhZQsCiMB7j6zsPH7m/AV0wtMdpTotjOS2Y=",
  },

  // Local builds use ad-hoc signing without a developer certificate.
  // Release builds opt in with MACOS_SIGNING_IDENTITY and, for notarization,
  // MACOS_NOTARY_PROFILE (from `xcrun notarytool store-credentials`).
  macos: {
    minimumSystemVersion: "13.0",
    signingIdentity,
    notarize: signingIdentity !== "-" && notaryProfile
      ? { keychainProfile: notaryProfile }
      : undefined,
  },
});

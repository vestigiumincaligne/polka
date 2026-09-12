import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  test: {
    environment: "jsdom",
    setupFiles: ["./src/test/setup.js"],
    css: false,
    // CI runners can be an order of magnitude slower than a dev machine;
    // the defaults (5s/1s) turn that into flaky failures.
    testTimeout: 15000,
    hookTimeout: 15000,
  },
});

import { defineConfig } from "@playwright/test";

// Runs the freshly built server against a throwaway SHUTTLE_HOME for the
// session. SHUTTLE_E2E=1 exposes POST /_test/reset.
export default defineConfig({
  testDir: "./tests",
  timeout: 30_000,
  fullyParallel: false,
  use: { baseURL: "http://127.0.0.1:8791", trace: "retain-on-failure" },
  webServer: {
    command:
      "rm -rf .tmp && mkdir -p .tmp && cd .. && " +
      "SHUTTLE_EMBED_AUTOSTART=0 SHUTTLE_E2E=1 SHUTTLE_HOME=./e2e/.tmp/home " +
      "SHUTTLE_ADDR=:8791 ./bin/shuttle",
    url: "http://127.0.0.1:8791/",
    reuseExistingServer: false,
    timeout: 20_000,
  },
});

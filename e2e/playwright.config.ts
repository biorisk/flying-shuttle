import { defineConfig } from "@playwright/test";

// Runs the freshly built server on a throwaway DB + index for the session.
export default defineConfig({
  testDir: "./tests",
  timeout: 30_000,
  fullyParallel: false,
  use: { baseURL: "http://127.0.0.1:8791", trace: "retain-on-failure" },
  webServer: {
    command:
      "mkdir -p .tmp/uploads && cd .. && SHUTTLE_EMBED_AUTOSTART=0 SHUTTLE_E2E=1 " +
      "SHUTTLE_DB=./e2e/.tmp/db.sqlite SHUTTLE_BM25_PATH=./e2e/.tmp/x.bm25 " +
      "SHUTTLE_HNSW_PATH=./e2e/.tmp/x.hnsw SHUTTLE_UPLOAD_DIR=./e2e/.tmp/uploads " +
      "SHUTTLE_ADDR=:8791 ./bin/shuttle",
    url: "http://127.0.0.1:8791/",
    reuseExistingServer: false,
    timeout: 20_000,
  },
});

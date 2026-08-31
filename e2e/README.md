# Browser end-to-end tests

Playwright tests for the server-rendered UI. Not part of the Go build.

```bash
npm ci
npx playwright install chromium
npx playwright test          # or, from the repo root: make e2e
```

`playwright.config.ts` builds nothing itself — run `make build` (or `go build
-o bin/shuttle ./cmd/shuttle`) first. It then starts `bin/shuttle` on port 8791
with a throwaway DB/index under `e2e/.tmp/`.

The suite is `test.describe.configure({ mode: "serial" })`: one browser session
walks the whole loop (keyboard outline editing → evidence retrieval →
highlight-to-excerpt attach → stitch preview). Serial because all tests share
one server process and one SQLite file.

The webServer points `SHUTTLE_HOME` at `e2e/.tmp/home` (wiped each run), so the
suite never touches your real `~/.shuttle`.

import { test, expect, type APIRequestContext } from "@playwright/test";

// One serial suite: a single browser session walks the whole product loop.
// Serial (not per-test isolated) because the tests share one server process
// and one SQLite file. The server is started with SHUTTLE_E2E=1, which exposes
// POST /_test/reset (there is no JSON CRUD API to reset through).
test.describe.configure({ mode: "serial" });

async function reset(request: APIRequestContext) {
  const res = await request.post("/_test/reset");
  if (!res.ok()) throw new Error(`reset failed: ${res.status()}`);
}

// Upload a transcript through the ingest drawer endpoint and wait for it to
// finish processing (status "done" in the drawer fragment).
async function uploadTranscript(request: APIRequestContext, name: string, text: string) {
  const r = await request.post("/ingest", {
    multipart: { files: { name, mimeType: "text/plain", buffer: Buffer.from(text) } },
  });
  if (!r.ok()) throw new Error(`upload failed: ${r.status()}`);
  await expect
    .poll(async () => (await (await request.get("/ingest")).text()).includes("status-done"), {
      timeout: 8000,
    })
    .toBe(true);
}

test("keyboard: Enter adds a sibling, Tab indents it", async ({ page, request }) => {
  await reset(request);
  await page.goto("/");
  await page.getByRole("button", { name: "Add the first bullet" }).click();

  const first = page.locator(".bullet-input").first();
  await expect(first).toBeFocused();
  await first.fill("Chapter one");
  await first.press("Enter");

  await expect(page.locator(".bullet-input")).toHaveCount(2);
  await expect(page.locator(".bullet-input").nth(1)).toBeFocused();

  await page.locator(".bullet-input").nth(1).fill("A sub point");
  await page.locator(".bullet-input").nth(1).press("Tab");
  await expect(page.locator(".bullet-children .bullet-input")).toHaveValue("A sub point");
});

test("typing a bullet surfaces evidence, and a highlighted span attaches as a locked sub-bullet", async ({
  page,
  request,
}) => {
  await reset(request);
  await uploadTranscript(
    request,
    "iv.txt",
    "I was terrified before the vote that morning.\n\n" +
      "But the moment I began to speak the terror became resolve and I carried the room.",
  );

  await page.goto("/");
  await page.getByRole("button", { name: "Add the first bullet" }).click();
  await page.locator(".bullet-input").first().fill("the terror before the vote");

  const candidate = page.locator("#evidence .candidate").first();
  await expect(candidate).toBeVisible({ timeout: 5000 });
  await expect(page.locator("#evidence")).toContainText("terror");

  await candidate.getByRole("button", { name: /read in transcript/i }).click();
  const reader = page.locator("#transcript-reader");
  await expect(reader).toBeVisible();

  const seg = reader.locator(".reader-seg", { hasText: "resolve" }).first();
  await seg.evaluate((el) => {
    const t = el.firstChild!;
    const i = el.textContent!.indexOf("became resolve");
    const r = document.createRange();
    r.setStart(t, i);
    r.setEnd(t, i + "became resolve".length);
    const sel = window.getSelection()!;
    sel.removeAllRanges();
    sel.addRange(r);
    document.dispatchEvent(new Event("selectionchange"));
  });
  await reader.getByRole("button", { name: "Add as evidence" }).click();

  const evidence = page.locator(".bullet.evidence .bullet-evidence");
  await expect(evidence).toBeVisible({ timeout: 5000 });
  await expect(evidence).toHaveText(/became resolve/);
  await expect(evidence).not.toHaveText(/terrified/);
});

test("preview stitches the attached passage", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("button", { name: "Preview" }).click();
  await expect(page.locator("#stitch")).toBeVisible();
  await expect(page.locator("#stitch")).toContainText("became resolve");
  await expect(page.locator("#stitch .span-glue")).toHaveCount(0); // single passage, no glue yet
});

test("outline preview renders as a formatted page with width + format controls", async ({ page, context }) => {
  await page.goto("/");
  const [preview] = await Promise.all([
    context.waitForEvent("page"),
    page.getByRole("link", { name: /outline/ }).click(),
  ]);
  await preview.waitForLoadState();
  await expect(preview.locator(".markdown-body h1").first()).toBeVisible();
  await expect(preview.locator('.doc-width input[value="landscape"]')).toHaveCount(1);
  await expect(preview.getByRole("link", { name: "rendered" })).toHaveClass(/active/);
  await expect(preview.getByRole("link", { name: "raw" })).toBeVisible();
  await expect(preview.getByRole("link", { name: "PDF" })).toBeVisible();
});

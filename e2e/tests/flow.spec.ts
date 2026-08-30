import { test, expect, type APIRequestContext } from "@playwright/test";

// One serial suite: a single browser session walks the whole product loop.
// Serial (not per-test isolated) because the tests share one server process
// and one SQLite file; interleaving fresh pages with a still-settling server
// produces spurious duplicate writes.
test.describe.configure({ mode: "serial" });

async function resetOutline(request: APIRequestContext) {
  for (let i = 0; i < 6; i++) {
    const { data } = await (await request.get("/api/v1/nodes")).json();
    if (!data?.length) return;
    for (const n of data) await request.delete(`/api/v1/nodes/${n.id}`);
    await new Promise((r) => setTimeout(r, 100));
  }
  throw new Error("could not clear outline");
}

test("keyboard: Enter adds a sibling, Tab indents it", async ({ page, request }) => {
  await resetOutline(request);
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
  await resetOutline(request);

  const body =
    "I was terrified before the vote that morning.\n\n" +
    "But the moment I began to speak the terror became resolve and I carried the room.";
  await request.post("/api/v1/uploads", {
    multipart: { file: { name: "iv.txt", mimeType: "text/plain", buffer: Buffer.from(body) } },
  });
  await expect
    .poll(async () => ((await (await request.get("/api/v1/chunks")).json()).data ?? []).length, {
      timeout: 8000,
    })
    .toBeGreaterThan(0);

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

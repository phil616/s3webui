// SPDX-License-Identifier: AGPL-3.0-only
// Run only against an isolated disposable PostgreSQL database.
import { chromium } from "playwright";
import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { mkdir } from "node:fs/promises";
import { createServer } from "node:net";
const dsn = process.env.BROWSER_DATABASE_URL;
if (!dsn)
  throw Error(
    "Set BROWSER_DATABASE_URL to an isolated disposable PostgreSQL database",
  );
const probe = createServer();
await new Promise((r) => probe.listen(0, "127.0.0.1", r));
const port = probe.address().port;
await new Promise((r) => probe.close(r));
const base = `http://127.0.0.1:${port}`;
const child = spawn(
  process.env.BROWSER_SERVER_BINARY ||
    `bin/s3webui${process.platform === "win32" ? ".exe" : ""}`,
  [],
  {
    env: {
      ...process.env,
      DATABASE_URL: dsn,
      HTTP_ADDR: `127.0.0.1:${port}`,
      PUBLIC_URL: base,
      ADMIN_EMAIL: "browser@example.com",
      ADMIN_PASSWORD: "browser-test-password",
      APP_ENCRYPTION_KEY:
        process.env.BROWSER_ENCRYPTION_KEY ||
        Buffer.alloc(32, 7).toString("base64"),
      SOURCE_URL: "https://example.com/test-source",
      COOKIE_SECURE: "false",
    },
    windowsHide: true,
    stdio: ["ignore", "pipe", "pipe"],
  },
);
let logs = "";
child.stdout.on("data", (b) => (logs += b));
child.stderr.on("data", (b) => (logs += b));
child.on("error", (e) => (logs += e));
let browser, page;
const errors = [];
try {
  for (let i = 0; i < 100; i++) {
    if (child.exitCode !== null) throw Error("Server exited: " + logs);
    try {
      if ((await fetch(base + "/healthz")).ok) break;
    } catch {}
    if (i === 99) throw Error("Server did not start: " + logs);
    await new Promise((r) => setTimeout(r, 100));
  }
  browser = await chromium.launch({
    headless: true,
    ...(process.env.BROWSER_EXECUTABLE
      ? { executablePath: process.env.BROWSER_EXECUTABLE }
      : {}),
  });
  const context = await browser.newContext({
    locale: "zh-CN",
    viewport: { width: 1440, height: 1000 },
  });
  page = await context.newPage();
  page.on("pageerror", (e) => errors.push(String(e)));
  page.on("dialog", (d) => d.accept());
  let completed = 0,
    aborted = 0,
    parts = 0;
  const key = "中文//../a+b #?%<img>.txt";
  await context.route(base + "/api/buckets**", async (route) => {
    const req = route.request(),
      u = new URL(req.url());
    let body = {};
    if (u.pathname === "/api/buckets")
      body = {
        Buckets: [
          { Name: "test-bucket", CreationDate: "2026-10-16T00:00:00Z" },
        ],
      };
    else if (u.pathname.endsWith("/multipart/initiate"))
      body = { UploadId: "upload+/=" };
    else if (u.pathname.endsWith("/multipart/part")) {
      parts++;
      assert.equal(u.searchParams.get("uploadId"), "upload+/=");
      body = { ETag: `"etag-${u.searchParams.get("partNumber")}"` };
    } else if (u.pathname.endsWith("/multipart/complete")) {
      completed++;
      assert.equal(req.postDataJSON().parts.length, 3);
      body = { ETag: '"multipart-etag"' };
    } else if (u.pathname.endsWith("/multipart/abort")) {
      aborted++;
      body = {};
    } else if (u.pathname.endsWith("/versions"))
      body = {
        Versions: [{ Key: key, VersionId: "null", Size: 3, IsLatest: true }],
        DeleteMarkers: [
          { Key: "deleted", VersionId: "marker", IsLatest: true },
        ],
        IsTruncated: false,
      };
    else if (u.pathname.includes("/config/")) body = { Status: "Enabled" };
    else if (u.pathname.endsWith("/objects"))
      body = {
        Contents: [{ Key: key, Size: 3, LastModified: "2026-10-16T00:00:00Z" }],
        CommonPrefixes: [{ Prefix: "folder/" }],
        IsTruncated: false,
      };
    else if (u.pathname.endsWith("/objects/head")) {
      assert.equal(u.searchParams.get("key"), key);
      body = { ETag: '"opaque"', Metadata: { note: "<script>bad</script>" } };
    }
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(body),
    });
  });
  await page.goto(base);
  await page.getByRole("heading", { name: "登录", exact: true }).waitFor();
  assert.equal(await page.locator("html").getAttribute("lang"), "zh-CN");
  assert.ok(
    await page.getByRole("link", { name: "获取当前版本源码" }).isVisible(),
  );
  await page.locator("#language").selectOption("en");
  await page.getByRole("heading", { name: "Sign in", exact: true }).waitFor();
  await page.reload();
  await page.getByRole("heading", { name: "Sign in", exact: true }).waitFor();
  await page.getByLabel("Email", { exact: true }).fill("browser@example.com");
  await page
    .getByLabel("Password", { exact: true })
    .fill("browser-test-password");
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await page.getByRole("heading", { name: "Buckets", exact: true }).waitFor();
  await page
    .getByRole("button", { name: "S3 connections", exact: true })
    .click();
  await page
    .getByRole("button", { name: "Create connection", exact: true })
    .click();
  const connectionName = "browser-" + Date.now();
  await page.getByLabel("Name", { exact: true }).fill(connectionName);
  await page.getByLabel("Access key", { exact: true }).fill("test");
  await page.getByLabel("Secret key", { exact: true }).fill("browser-secret");
  await page.getByRole("button", { name: "Save", exact: true }).click();
  await page.locator("#dialog").waitFor({ state: "hidden" });
  await page.locator("#connection").selectOption({ label: connectionName });
  await page.getByRole("button", { name: "▧ test-bucket" }).waitFor();
  await page.getByRole("button", { name: "Open bucket",exact:true }).click();
  await page.getByLabel("Bucket name",{exact:true}).fill("test-bucket");
  await page.getByRole("button",{name:"Load",exact:true}).click();
  await page.locator("#dialog").waitFor({state:"hidden"});
  await page.getByRole("heading", { name: "test-bucket" }).waitFor();
  await page.getByRole("cell", { name: key, exact: true }).waitFor();
  assert.equal(await page.locator("#content img").count(), 0);
  await page
    .getByRole("button", { name: "Object metadata", exact: true })
    .click();
  await page.locator("dialog pre").waitFor();
  assert.match(
    await page.locator("dialog pre").textContent(),
    /<script>bad<\/script>/,
  );
  assert.equal(await page.locator("dialog script").count(), 0);
  await page.getByRole("button", { name: "Cancel", exact: true }).click();
  await page.getByRole("button", { name: "Versions", exact: true }).click();
  await page.getByText("Delete marker", { exact: true }).waitFor();
  await page.getByRole("button", { name: "Versions", exact: true }).click();
  await page.locator("#files").setInputFiles({
    name: "large.bin",
    mimeType: "application/octet-stream",
    buffer: Buffer.alloc(17 * 1024 ** 2),
  });
  await page.waitForFunction(
    () => !document.getElementById("upload-status")?.textContent,
    { timeout: 30000 },
  );
  for (let i = 0; i < 100 && completed === 0; i++)
    await page.waitForTimeout(100);
  assert.equal(completed, 1);
  assert.equal(parts, 3);
  assert.equal(aborted, 0);
  await page.locator("#language").selectOption("zh");
  await page.getByRole("button", { name: "存储桶配置", exact: true }).click();
  await page
    .getByRole("heading", { name: "存储桶配置", exact: true })
    .waitFor();
  await page.locator("#config-kind").selectOption("versioning");
  await page.getByRole("button", { name: "读取", exact: true }).click();
  await page.waitForFunction(() =>
    document.getElementById("config-document").value.includes("Enabled"),
  );
  await page.getByRole("button", { name: "取消", exact: true }).click();
  await mkdir(".local/screenshots", { recursive: true });
  await page.screenshot({
    path: ".local/screenshots/objects-zh.png",
    fullPage: true,
  });
  await page.setViewportSize({ width: 390, height: 844 });
  assert.ok(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
    "Mobile page overflows horizontally",
  );
  await page.screenshot({
    path: ".local/screenshots/mobile-zh.png",
    fullPage: true,
  });
  assert.ok(
    await page
      .getByRole("button", { name: "上传文件", exact: true })
      .isVisible(),
  );
  await page.getByRole("button", { name: "退出登录", exact: true }).click();
  await page.getByRole("heading", { name: "登录", exact: true }).waitFor();
  assert.deepEqual(errors, []);
  console.log(
    "Browser checks passed: login, Chinese/English persistence, connection creation, object escaping, metadata, versions, multipart upload, bucket configuration, mobile and logout.",
  );
} catch (e) {
  console.error(
    "Browser errors:",
    errors,
    "Server:",
    logs,
    "Page:",
    await page?.locator("body").innerText(),
  );
  throw e;
} finally {
  if (browser) await browser.close();
  child.kill();
}

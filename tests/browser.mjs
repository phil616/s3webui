// SPDX-License-Identifier: AGPL-3.0-only
import { chromium } from "playwright";
import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { randomBytes } from "node:crypto";
import { mkdir } from "node:fs/promises";
import { createServer } from "node:http";
import pg from "pg";
const dsn = process.env.BROWSER_DATABASE_URL;
if (!dsn) throw Error("Set BROWSER_DATABASE_URL to an isolated test database");
const db = new pg.Client({ connectionString: dsn });
await db.connect();
const schema = "browser_" + randomBytes(8).toString("hex");
await db.query("CREATE SCHEMA " + schema);
const databaseURL = new URL(dsn);
databaseURL.searchParams.set("search_path", schema);
const key = "a+b #?%<img>.txt";
let complete = 0,
  parts = 0,
  put = 0,
  copies = 0,
  deletes = 0;
const xml = (s) =>
  String(s).replace(
    /[&<>"']/g,
    (c) =>
      ({
        "&": "&amp;",
        "<": "&lt;",
        ">": "&gt;",
        '"': "&quot;",
        "'": "&apos;",
      })[c],
  );
const s3 = createServer(async (req, res) => {
  try {
    const u = new URL(req.url, "http://s3.test");
    const b = [];
    for await (const c of req) b.push(c);
    const body = Buffer.concat(b);
    res.setHeader("Content-Type", "application/xml");
    if (req.method === "GET" && u.pathname === "/") {
      res.end(
        "<ListAllMyBucketsResult><Buckets><Bucket><Name>design-test</Name><CreationDate>2026-10-16T00:00:00Z</CreationDate></Bucket></Buckets></ListAllMyBucketsResult>",
      );
      return;
    }
    if (req.method === "GET" && u.searchParams.get("list-type") === "2") {
      const prefix = u.searchParams.get("prefix") ?? "";
      res.end(
        "<ListBucketResult><Name>design-test</Name><IsTruncated>false</IsTruncated>" +
          (prefix
            ? ""
            : `<Contents><Key>${xml(key)}</Key><Size>3</Size><LastModified>2026-10-16T00:00:00Z</LastModified></Contents><Contents><Key>Delete</Key><Size>1</Size><LastModified>2026-10-16T00:00:00Z</LastModified></Contents>`) +
          (u.searchParams.get("delimiter") === "/" && !prefix
            ? "<CommonPrefixes><Prefix>reports/</Prefix></CommonPrefixes>"
            : "") +
          "</ListBucketResult>",
      );
      return;
    }
    if (req.method === "HEAD") {
      res.setHeader("ETag", '"source-etag"');
      res.setHeader("Content-Length", "3");
      res.end();
      return;
    }
    if (req.method === "POST" && u.searchParams.has("uploads")) {
      res.end(
        "<InitiateMultipartUploadResult><Bucket>design-test</Bucket><Key>large.bin</Key><UploadId>upload+/=</UploadId></InitiateMultipartUploadResult>",
      );
      return;
    }
    if (req.method === "PUT" && u.searchParams.has("partNumber")) {
      parts++;
      res.setHeader("ETag", '"opaque-' + parts + '"');
      res.end();
      return;
    }
    if (req.method === "POST" && u.searchParams.has("uploadId")) {
      complete++;
      res.end(
        '<CompleteMultipartUploadResult><Bucket>design-test</Bucket><Key>large.bin</Key><ETag>"complete"</ETag></CompleteMultipartUploadResult>',
      );
      return;
    }
    if (req.method === "PUT" && req.headers["x-amz-copy-source"]) {
      copies++;
      res.end('<CopyObjectResult><ETag>"copy"</ETag></CopyObjectResult>');
      return;
    }
    if (req.method === "PUT") {
      put++;
      res.setHeader("ETag", '"put"');
      res.end();
      return;
    }
    if (req.method === "DELETE") {
      deletes++;
      res.statusCode = 204;
      res.end();
      return;
    }
    res.statusCode = 404;
    res.end("<Error><Code>NoSuchBucketPolicy</Code></Error>");
  } catch (e) {
    res.statusCode = 500;
    res.end(String(e));
  }
});
await new Promise((r) => s3.listen(0, "127.0.0.1", r));
const s3URL = "http://127.0.0.1:" + s3.address().port;
const probe = createServer();
await new Promise((r) => probe.listen(0, "127.0.0.1", r));
const port = probe.address().port;
await new Promise((r) => probe.close(r));
const base = "http://127.0.0.1:" + port;
const child = spawn(
  process.env.BROWSER_SERVER_BINARY ||
    `bin/s3webui${process.platform === "win32" ? ".exe" : ""}`,
  [],
  {
    env: {
      ...process.env,
      DATABASE_URL: databaseURL.toString(),
      HTTP_ADDR: "127.0.0.1:" + port,
      PUBLIC_URL: base,
      ADMIN_EMAIL: "browser@example.com",
      ADMIN_PASSWORD: "browser-test-password",
      APP_ENCRYPTION_KEY: Buffer.alloc(32, 7).toString("base64"),
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
await mkdir(".local/screenshots/restored", { recursive: true });
const screenshot = async (name) =>
  page.screenshot({
    path: ".local/screenshots/restored/" + name + ".png",
    fullPage: true,
  });
const switchLanguage = async (language) => {
  await page.waitForFunction(() => Boolean(window.__s3i18n));
  await Promise.all([
    page.waitForEvent("load"),
    page.locator("[data-language]").selectOption(language),
  ]);
  assert.equal(await page.locator("html").getAttribute("lang"), language);
};
try {
  for (let i = 0; i < 100; i++) {
    if (child.exitCode !== null) throw Error(logs);
    try {
      if ((await fetch(base + "/healthz")).ok) break;
    } catch {}
    await new Promise((r) => setTimeout(r, 100));
    if (i === 99) throw Error(logs);
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
  page.on("dialog", (d) => d.accept("new-name.txt"));
  await page.goto(base);
  await page.getByText("欢迎回来", { exact: true }).waitFor();
  await screenshot("login-zh");
  assert.equal(await page.locator("html").getAttribute("lang"), "zh");
  await switchLanguage("en");
  await page.getByText("Welcome back", { exact: true }).waitFor();
  await screenshot("login-en");
  await page.reload();
  await page.getByText("Welcome back", { exact: true }).waitFor();
  await page.locator("#email").fill("browser@example.com");
  await page.locator("#password").fill("browser-test-password");
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await page.locator(".app-topbar").waitFor();
  assert.equal(await page.locator(".topbar-inner").count(), 1);
  assert.equal(await page.locator(".layout aside").count(), 0);
  await page
    .getByRole("link", { name: "Settings", exact: true })
    .first()
    .click();
  await page.locator("#adminTabBtn-keys").waitFor();
  await page.getByRole("button", { name: "New key", exact: true }).click();
  await page.locator("#credentialModal:not(.hidden)").waitFor();
  await page.locator("#credentialName").fill("Original design");
  await page.locator("#credentialEndpoint").fill(s3URL);
  await page.locator("#credentialAccessKey").fill("test-access");
  await page.locator("#credentialSecretKey").fill("test-secret");
  await page
    .locator("#credentialModal")
    .getByRole("button", { name: "Save", exact: true })
    .click();
  await page.locator("#credentialModal").waitFor({ state: "hidden" });
  await page.getByText("Original design", { exact: true }).first().waitFor();
  await screenshot("settings-en");
  await page
    .getByRole("link", { name: "Buckets", exact: true })
    .first()
    .click();
  await page.locator(".bucket-card").first().waitFor();
  await page.locator(".bucket-card .bucket-name").first().waitFor();
  await screenshot("buckets-en");
  await page.locator("#viewTableBtn").click();
  assert.ok(await page.locator("#bucketTableView").isVisible());
  await page.locator("#viewPieBtn").click();
  await page
    .locator("#bucketPieChart path")
    .first()
    .waitFor({ state: "attached" });
  assert.ok(await page.locator("#bucketPieView").isVisible());
  await page.locator("#viewCardsBtn").click();
  await switchLanguage("zh");
  await page.getByRole("button", { name: "新建存储桶", exact: true }).waitFor();
  await screenshot("buckets-zh");
  await page.locator(".bucket-name").first().click();
  await page.locator("#objectTableBody").waitFor();
  await page.getByText(key, { exact: true }).waitFor();
  assert.equal(await page.locator("#objectTableBody img").count(), 0);
  await page.getByText("Delete", { exact: true }).waitFor();
  await screenshot("objects-zh");
  await switchLanguage("en");
  await page.getByText(key, { exact: true }).waitFor();
  await screenshot("objects-en");
  await page.locator("#objectSearch").fill("Delete");
  assert.equal(await page.locator("#objectTableBody tr:visible").count(), 1);
  await page.locator("#objectSearch").fill("");
  await page.getByRole("button", { name: "Upload", exact: true }).click();
  await page.locator("#uploadModal:not(.hidden)").waitFor();
  await page
    .locator("#uploadModal input[type=file]")
    .setInputFiles({
      name: "large.bin",
      mimeType: "application/octet-stream",
      buffer: Buffer.alloc(17 * 1024 ** 2),
    });
  await page.locator("#startUploadBtn").click();
  await page.getByText(/Upload complete:/).waitFor({ timeout: 30000 });
  assert.equal(complete, 1);
  assert.equal(parts, 4);
  await page.locator("#uploadModal .modal-header button").click();
  await page.locator("#objectTableBody").waitFor();
  await page
    .getByRole("link", { name: "Settings", exact: true })
    .first()
    .click();
  await switchLanguage("zh");
  await page.locator("#adminTabBtn-users").click();
  await page.locator("#userTableBody tr").first().waitFor();
  await screenshot("settings-zh");
  await page
    .getByRole("link", { name: "历史记录", exact: true })
    .first()
    .click();
  await page.locator("#historyTableBody").waitFor();
  await screenshot("history-zh");
  await page.getByRole("link", { name: "存储桶", exact: true }).first().click();
  await page.setViewportSize({ width: 390, height: 844 });
  await page.locator(".bucket-card").first().waitFor();
  await screenshot("mobile-zh");
  assert.ok(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  );
  assert.deepEqual(errors, []);
  console.log(
    "Restored upstream UI passed: original login/topbar/cards/table/chart/object controls/settings/history; bilingual text, raw data preservation and actual Go/S3 multipart requests.",
  );
} catch (e) {
  console.error(
    "Page errors:",
    errors,
    "Server:",
    logs,
    "Body:",
    await page?.locator("body").innerText(),
  );
  if (page) await screenshot("failure");
  throw e;
} finally {
  if (browser) await browser.close();
  const exited = new Promise((r) => child.once("exit", r));
  if (child.exitCode === null) {
    child.kill();
    await exited;
  }
  await new Promise((r) => s3.close(r));
  await db.query("DROP SCHEMA " + schema + " CASCADE");
  await db.end();
}

// SPDX-License-Identifier: AGPL-3.0-only
import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import {
  messages,
  translate,
  normalizeLocale,
  initialLocale,
  formatDate,
  formatSize,
} from "../internal/app/web/i18n.mjs";
import {
  uploadPlan,
  objectQuery,
  escapeHTML,
  uploadFile,
} from "../internal/app/web/upload.mjs";

test("only Chinese and English; all keys and placeholders match", () => {
  assert.deepEqual(Object.keys(messages).sort(), ["en", "zh"]);
  assert.deepEqual(
    Object.keys(messages.zh).sort(),
    Object.keys(messages.en).sort(),
  );
  for (const key of Object.keys(messages.en)) {
    assert.ok(messages.zh[key]);
    assert.deepEqual(
      messages.en[key].match(/\{\w+\}/g) || [],
      messages.zh[key].match(/\{\w+\}/g) || [],
      key,
    );
  }
});

test("failed multipart upload aborts the pinned connection and preserves ETags", async () => {
  const original = globalThis.XMLHttpRequest;
  let sent = 0;
  const calls = [];
  class FakeXHR {
    constructor() {
      this.upload = {};
      this.headers = {};
    }
    open(method, url) {
      this.url = url;
    }
    setRequestHeader(k, v) {
      this.headers[k] = v;
    }
    send() {
      sent++;
      assert.equal(this.headers["X-S3-Credential-ID"], "42");
      queueMicrotask(() => {
        this.status = sent === 2 ? 500 : 200;
        this.responseText =
          sent === 2 ? '{"code":"InternalError"}' : '{"ETag":"opaque-etag"}';
        this.onload();
        this.onloadend();
      });
    }
    abort() {
      this.onabort();
      this.onloadend();
    }
  }
  globalThis.XMLHttpRequest = FakeXHR;
  try {
    const api = async (url, opts) => {
      calls.push({ url, opts });
      assert.equal(opts.headers["X-S3-Credential-ID"], "42");
      return { UploadId: "opaque+/=" };
    };
    await assert.rejects(
      () =>
        uploadFile(
          "bucket",
          "key",
          new Blob([new Uint8Array(17 * 1024 ** 2)]),
          api,
          new AbortController().signal,
          () => {},
          42,
        ),
      { code: "InternalError" },
    );
    assert.equal(sent, 2);
    assert.equal(calls.length, 2);
    assert.ok(calls[1].url.includes("multipart/abort"));
    assert.equal(calls[1].opts.method, "DELETE");
    assert.equal(calls[1].opts.signal, undefined);
  } finally {
    globalThis.XMLHttpRequest = original;
  }
});
test("literal UI and API error translation keys exist", () => {
  const code = readFileSync(
    new URL("../internal/app/web/app.mjs", import.meta.url),
    "utf8",
  );
  const keys = [...code.matchAll(/\bt\(["']([^"']+)["']/g)];
  assert.ok(keys.length > 30);
  for (const [, key] of keys) assert.ok(messages.en[key], key);
  for (const file of ["server.go", "admin.go", "s3.go", "bucket_config.go"]) {
    const source = readFileSync(
      new URL("../internal/app/" + file, import.meta.url),
      "utf8",
    );
    for (const [, key] of source.matchAll(
      /(?:bad\(|&apiError\{\d+,\s*)"([A-Za-z]+)"/g,
    ))
      assert.ok(messages.en["error_" + key], key);
  }
});
test("locale preference survives unavailable storage and unsupported language", () => {
  assert.equal(normalizeLocale("zh-Hans-CN"), "zh");
  assert.equal(normalizeLocale("fr-FR"), "en");
  assert.equal(initialLocale({ getItem: () => "zh" }, ["en"]), "zh");
  assert.equal(
    initialLocale(
      {
        getItem() {
          throw Error();
        },
      },
      ["zh-CN"],
    ),
    "zh",
  );
  assert.equal(initialLocale({ getItem: () => "ja" }, ["de"]), "en");
  assert.equal(
    translate("zh", "uploading", { name: "a.txt", percent: 25 }),
    "正在上传 a.txt：25%",
  );
  assert.throws(() => translate("en", "missing"));
  assert.match(formatSize(1024, "en"), /1 KiB/);
  assert.ok(formatDate("2026-10-16T12:00:00Z", "zh"));
});
test("multipart planning uses binary minimum size and never exceeds 10000 parts", () => {
  assert.equal(uploadPlan(0).multipart, false);
  assert.equal(uploadPlan(16 * 1024 ** 2).multipart, false);
  for (const size of [16 * 1024 ** 2 + 1, 80 * 1024 ** 3, 625 * 1024 ** 3]) {
    const p = uploadPlan(size);
    assert.ok(p.partSize >= 5 * 1024 ** 2);
    assert.ok(p.partSize <= 64 * 1024 ** 2);
    assert.ok(p.count <= 10000);
  }
  assert.throws(() => uploadPlan(625 * 1024 ** 3 + 1));
});
test("object query preserves opaque keys and tokens, including null version IDs", () => {
  const key = "中文//../a+b #?%";
  const u = new URL(
    objectQuery("bucket", { key, versionId: "null", token: "+/=" }),
    "https://example.com",
  );
  assert.equal(u.searchParams.get("key"), key);
  assert.equal(u.searchParams.get("versionId"), "null");
  assert.equal(u.searchParams.get("token"), "+/=");
  assert.equal(
    escapeHTML('<img src=x onerror="x">'),
    "&lt;img src=x onerror=&quot;x&quot;&gt;",
  );
});

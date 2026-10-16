// SPDX-License-Identifier: AGPL-3.0-only
import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync, existsSync } from "node:fs";
import { execFileSync } from "node:child_process";
const en = JSON.parse(
  readFileSync("src/main/resources/locales/en.json", "utf8"),
);
const zh = JSON.parse(
  readFileSync("src/main/resources/locales/zh.json", "utf8"),
);
const baseline = "a820d5945127cfc60b48e3c03c5eb1e54e09fc17";
const normalized = (s) => s.replace(/\r\n/g, "\n");
test("the actual original stylesheet, logos and core page templates are retained", () => {
  for (const file of [
    "src/main/frontend/s3webui.css",
    "src/main/resources/static/img/logo.png",
    "src/main/resources/templates/buckets.html",
    "src/main/resources/templates/bucket.html",
    "src/main/resources/templates/history.html",
    "src/main/resources/templates/search.html",
    "src/main/resources/templates/admin/settings.html",
    "src/main/resources/templates/admin/fragments/iam.html",
  ]) {
    const original = execFileSync("git", ["show", baseline + ":" + file]);
    const current = readFileSync(file);
    if (file.endsWith(".png")) assert.deepEqual(current, original, file);
    else
      assert.equal(
        normalized(current.toString()),
        normalized(original.toString()),
        file,
      );
  }
});
test("the replacement SPA is removed and original frontend is the build input", () => {
  for (const file of ["app.mjs", "index.html", "style.css", "i18n.mjs"])
    assert.equal(existsSync("internal/app/web/" + file), false, file);
  const build = readFileSync("scripts/build-original-ui.mjs", "utf8");
  assert.ok(build.includes("src/main/frontend/s3webui.js"));
  assert.ok(build.includes("src/main/frontend/s3webui.css"));
  assert.ok(build.includes("src/main/resources/templates"));
});
test("Chinese and English catalog keys match and translations never introduce data placeholders", () => {
  assert.deepEqual(Object.keys(en).sort(), Object.keys(zh).sort());
  assert.ok(Object.keys(en).length > 400);
  for (const [key, value] of Object.entries(zh)) {
    assert.ok(value, key);
    const original = new Set(key.match(/\{\d+\}/g) ?? []);
    for (const p of value.match(/\{\d+\}/g) ?? [])
      assert.ok(original.has(p), key + " " + p);
  }
  for (const key of [
    "Welcome back",
    "New bucket",
    "Bucket sizes",
    "History",
    "Settings",
    "Upload files",
    "New S3 key",
    "Delete selected ({0})",
  ])
    assert.notEqual(zh[key], en[key], key);
});
test("catalog preserves technical identifiers, protocol values and CSS classes", () => {
  for (const key of [
    "S3WEBUI",
    "S3",
    "CORS",
    "ARN",
    "USER",
    "us-east-1",
    "btn-icon shrink-0",
  ])
    assert.equal(zh[key], key, key);
  assert.ok(
    readFileSync("src/main/frontend/i18n.js", "utf8").includes(
      "values[Number(i)]",
    ),
  );
});

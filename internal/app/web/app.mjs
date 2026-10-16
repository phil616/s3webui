// SPDX-License-Identifier: AGPL-3.0-only
import {
  messages,
  initialLocale,
  translate,
  formatDate,
  formatSize,
} from "./i18n.mjs";
import { escapeHTML as h, objectQuery, uploadFile } from "./upload.mjs";

let locale = initialLocale();
let theme = "light";
try {
  theme = localStorage.getItem("s3webui-theme") || "light";
} catch {}
document.documentElement.dataset.theme = theme;
const t = (key, params) => translate(locale, key, params);
const root = document.getElementById("app");
const dialog = document.getElementById("dialog");
let me = null,
  meta = {},
  connections = [],
  view = "buckets",
  bucket = "",
  prefix = "",
  token = "",
  tokens = [],
  data = {},
  rows = [],
  searchQuery = "",
  recursive = false,
  versionMode = false,
  pending = false,
  requestID = 0;
let uploadController = null;
const errorText = (err) =>
  messages.en[`error_${err.code}`]
    ? t(`error_${err.code}`)
    : t("failed", { code: err.code ?? err.message ?? "NetworkError" });
async function api(path, options = {}) {
  const init = { ...options, headers: { ...options.headers } };
  if (options.body !== undefined) {
    init.body = JSON.stringify(options.body);
    init.headers["Content-Type"] = "application/json";
  }
  const response = await fetch(path, init);
  if (response.status === 204) return null;
  let result;
  try {
    result = await response.json();
  } catch {
    result = { code: String(response.status) };
  }
  if (!response.ok) {
    if (response.status === 401 && path !== "/api/login") {
      me = null;
      render();
    }
    throw Object.assign(new Error(result.code), {
      code: result.code,
      status: response.status,
    });
  }
  return result;
}
let noticeTimer;
function notice(text) {
  const el = document.getElementById("notice");
  el.textContent = text;
  el.classList.add("visible");
  clearTimeout(noticeTimer);
  noticeTimer = setTimeout(() => el.classList.remove("visible"), 6500);
}
function button(action, label, extra = "", cls = "") {
  return `<button type="button" data-action="${action}" class="${cls}" ${extra}>${h(t(label))}</button>`;
}
function selectLocale() {
  return `<label class="check"><span class="muted">${h(t("language"))}</span><select id="language" aria-label="${h(t("language"))}">${["zh", "en"].map((l) => `<option value="${l}" ${locale === l ? "selected" : ""}>${h(t(l))}</option>`).join("")}</select></label>`;
}
function footer() {
  return `<footer><a href="${h(meta.upstream || "https://github.com/wenisch-tech/s3webui")}" target="_blank" rel="noopener noreferrer">${h(t("upstream"))}</a><br><a href="/LICENSE" target="_blank" rel="noopener">${h(t("license"))}</a><a href="${h(meta.sourceUrl || "#")}" target="_blank" rel="noopener noreferrer">${h(t("source"))}</a></footer>`;
}
function render() {
  document.documentElement.lang = locale === "zh" ? "zh-CN" : "en";
  const header = `<header class="topbar"><div class="brand"><span>▧</span> S3 Web UI</div><span class="muted">${h(t("subtitle"))}</span><div class="spacer"></div>${selectLocale()}${button("theme", "theme", "", "small")}${me ? button("logout", "logout", "", "small") : ""}</header>`;
  if (!me) {
    root.innerHTML =
      header +
      `<section class="panel login"><h1>${h(t("login"))}</h1><p class="muted">${h(t("subtitle"))}</p><form id="login-form"><label>${h(t("email"))}<input name="email" type="email" required autocomplete="username"></label><label>${h(t("password"))}<input name="password" type="password" required autocomplete="current-password" maxlength="72"></label><p class="error" id="login-error" role="alert"></p><button class="primary" type="submit">${h(t("login"))}</button></form></section>` +
      footer();
    return;
  }
  const nav = [
    "buckets",
    ...(me.role === "ADMIN" ? ["credentials", "users"] : []),
    "audit",
  ];
  root.innerHTML =
    header +
    `<div class="layout"><aside><nav>${nav.map((v) => button(`nav-${v}`, v, "", view === v ? "active" : "")).join("")}</nav></aside><main><div class="heading"><div><h1>${h(bucket && view === "buckets" ? bucket : t(view))}</h1><span class="muted">${h(me.email)}</span></div>${button("refresh", "refresh")}</div><div class="toolbar"><label class="check">${h(t("selectConnection"))}<select id="connection"><option value="">—</option>${connections.map((c) => `<option value="${c.id}" ${me.credentialId === c.id ? "selected" : ""}>${h(c.name)}</option>`).join("")}</select></label></div><div id="content" aria-busy="${pending}"></div></main></div>` +
    footer();
  renderContent();
}
function table(headers, body) {
  return `<div class="table-wrap"><table><thead><tr>${headers.map((k) => `<th>${h(t(k))}</th>`).join("")}</tr></thead><tbody>${body || `<tr><td colspan="${headers.length}" class="empty">${h(t("empty"))}</td></tr>`}</tbody></table></div>`;
}
function pager(hasNext) {
  return `<div class="pager">${tokens.length ? button("previous", "previous") : ""}<span class="muted">${h(t("page", { number: tokens.length + 1 }))}</span>${hasNext ? button("next", "next") : ""}</div>`;
}
function itemButtons(i, version = false) {
  return (
    button(`download-${i}`, "download", "", "small") +
    button(`details-${i}`, "details", "", "small") +
    (version
      ? ""
      : button(`copy-${i}`, "copy", "", "small") +
        button(`move-${i}`, "move", "", "small")) +
    button(`delete-object-${i}`, "delete", "", "small danger")
  );
}
function renderContent() {
  const content = document.getElementById("content");
  if (!content) return;
  if (pending) {
    content.innerHTML = `<div class="panel empty">${h(t("loading"))}</div>`;
    return;
  }
  if (view === "buckets") {
    if (!me.credentialId) {
      content.innerHTML = `<div class="panel empty">${h(connections.length ? t("selectConnection") : t("noConnections"))}</div>`;
      return;
    }
    if (!bucket) {
      rows = data.Buckets || [];
      content.innerHTML = `<div class="toolbar">${button("create-bucket", "createBucket", "", "primary")}${button("open-bucket", "openBucket")}</div><div class="panel">${table(["name", "created", "actions"], rows.map((b, i) => `<tr><td><button class="link key" data-action="bucket-${i}">▧ ${h(b.Name)}</button></td><td>${h(formatDate(b.CreationDate, locale))}</td><td>${button(`delete-bucket-${i}`, "delete", "", "small danger")}</td></tr>`).join(""))}${pager(!!data.ContinuationToken)}</div>`;
      return;
    }
    const toolbar = `<div class="breadcrumb">${button("back", "back", "", "small")} <span class="key">/ ${h(prefix)}</span></div><div class="toolbar">${button("upload", "upload", "", "primary")}${button("folder", "folder")}${button("settings", "settings")}${button("versions", "versions", "", versionMode ? "active" : "")}${button("pending", "pendingUploads")}</div>`;
    if (pendingUploadsView()) {
      rows = data.Uploads || [];
      content.innerHTML =
        toolbar +
        `<div class="panel">${table(["key", "uploadId", "actions"], rows.map((o, i) => `<tr><td class="key">${h(o.Key)}</td><td class="key">${h(o.UploadId)}</td><td>${button(`abort-${i}`, "abort", "", "small danger")}</td></tr>`).join(""))}${pager(data.IsTruncated)}</div>`;
      return;
    }
    rows = versionMode
      ? [
          ...(data.Versions || []).map((o) => ({ ...o, marker: false })),
          ...(data.DeleteMarkers || []).map((o) => ({ ...o, marker: true })),
        ]
      : data.Contents || [];
    const search = `<form id="search-form" class="toolbar"><label>${h(t("prefix"))}<input name="prefix" value="${h(prefix)}" class="key"></label>${!versionMode ? `<label>${h(t("search"))}<input name="q" value="${h(searchQuery)}"></label><label class="check"><input name="recursive" type="checkbox" ${recursive ? "checked" : ""}>${h(t("recursive"))}</label>` : ""}<button type="submit">${h(t("filter"))}</button></form>`;
    let body =
      !versionMode && !searchQuery
        ? (data.CommonPrefixes || [])
            .map(
              (o, i) =>
                `<tr><td><button class="link key" data-action="prefix-${i}">▸ ${h(o.Prefix)}</button></td><td>—</td><td>—</td><td></td></tr>`,
            )
            .join("")
        : "";
    body += rows
      .map(
        (o, i) =>
          `<tr><td class="key">${h(o.Key)}${versionMode ? `<br><span class="muted">${h(o.VersionId)}</span> ${o.IsLatest ? `<span class="badge">${h(t("latest"))}</span>` : ""} ${o.marker ? `<span class="badge">${h(t("deleteMarker"))}</span>` : ""}` : ""}</td><td>${o.marker ? "—" : h(formatSize(o.Size || 0, locale))}</td><td>${h(formatDate(o.LastModified, locale))}</td><td class="row-actions">${o.marker ? button(`delete-object-${i}`, "delete", "", "small danger") : itemButtons(i, versionMode)}</td></tr>`,
      )
      .join("");
    content.innerHTML =
      toolbar +
      search +
      (versionMode ? `<p class="hint">${h(t("versionsHint"))}</p>` : "") +
      (searchQuery ? `<p class="hint">${h(t("searchHint"))}</p>` : "") +
      `<div class="upload-status" id="upload-status"></div><div class="panel">${table(["key", "size", "modified", "actions"], body)}${pager(data.IsTruncated)}</div><input id="files" type="file" multiple hidden>`;
  } else if (view === "credentials") {
    rows = Array.isArray(data) ? data : [];
    content.innerHTML = `<div class="toolbar">${button("new-credential", "newConnection", "", "primary")}</div><div class="panel">${table(["name", "endpoint", "region", "grants", "actions"], rows.map((c, i) => `<tr><td>${h(c.name)} ${c.enabled ? "" : `<span class="badge">${h(t("enabled"))}: ✕</span>`}</td><td>${h(c.endpoint || "AWS S3")}</td><td>${h(c.region)}</td><td>${h(c.grants.join(", "))}</td><td>${button(`edit-credential-${i}`, "edit", "", "small")}${button(`delete-credential-${i}`, "delete", "", "small danger")}</td></tr>`).join(""))}</div>`;
  } else if (view === "users") {
    rows = Array.isArray(data) ? data : [];
    content.innerHTML = `<div class="toolbar">${button("new-user", "newUser", "", "primary")}</div><div class="panel">${table(["email", "role", "enabled", "actions"], rows.map((u, i) => `<tr><td>${h(u.email)}</td><td>${h(t(u.role === "ADMIN" ? "admin" : "user"))}</td><td>${u.enabled ? "✓" : "✕"}</td><td>${button(`edit-user-${i}`, "edit", "", "small")}${button(`delete-user-${i}`, "delete", "", "small danger")}</td></tr>`).join(""))}</div>`;
  } else {
    rows = Array.isArray(data) ? data : [];
    content.innerHTML = `<div class="panel">${table(["date", "actor", "action", "bucketName", "key", "status"], rows.map((o) => `<tr><td>${h(formatDate(o.date, locale))}</td><td>${h(o.actor)}</td><td>${h(o.action)}</td><td>${h(o.bucket)}</td><td class="key">${h(o.key)}</td><td>${h(t(o.code === "OK" ? "operationOK" : "operationFailed"))}</td></tr>`).join(""))}${pager(rows.length === 100)}</div>`;
  }
}
let unfinished = false;
function pendingUploadsView() {
  return unfinished && view === "buckets" && bucket;
}
function resetPage() {
  token = "";
  tokens = [];
}
function setUploadBusy() {
  root.querySelectorAll("button, select, input").forEach((el) => {
    el.disabled = true;
  });
}
async function load() {
  const id = ++requestID;
  if (!me) {
    render();
    return;
  }
  pending = true;
  render();
  try {
    let path;
    if (view === "credentials") path = "/api/admin/credentials";
    else if (view === "users") path = "/api/admin/users";
    else if (view === "audit")
      path = `/api/audit?${new URLSearchParams(token ? { before: token } : {})}`;
    else if (!me.credentialId) {
      data = {};
      return;
    } else if (!bucket)
      path = `/api/buckets?${new URLSearchParams(token ? { token } : {})}`;
    else if (unfinished) {
      const markers = token ? JSON.parse(token) : {};
      path = objectQuery(bucket, { path: "multipart", prefix, ...markers });
    } else if (versionMode) {
      const markers = token ? JSON.parse(token) : {};
      path = objectQuery(bucket, { path: "versions", prefix, ...markers });
    } else if (searchQuery)
      path = `/api/search?${new URLSearchParams({ bucket, prefix, q: searchQuery, token })}`;
    else path = objectQuery(bucket, { prefix, token, recursive });
    const result = await api(path);
    if (id === requestID) data = result;
  } catch (e) {
    if (id === requestID) {
      data = {};
      notice(errorText(e));
    }
  } finally {
    if (id === requestID) {
      pending = false;
      render();
    }
  }
}
function showDialog(title, body, submit, submitLabel = "save") {
  dialog.innerHTML = `<form id="dialog-form"><h2 id="dialog-title">${h(t(title))}</h2>${body}<p id="dialog-error" class="error" role="alert"></p><div class="dialog-actions">${button("close-dialog", "cancel")}<button type="submit" class="primary">${h(t(submitLabel))}</button></div></form>`;
  document.getElementById("dialog-form").onsubmit = async (e) => {
    e.preventDefault();
    const button = e.submitter;
    button.disabled = true;
    try {
      await submit(new FormData(e.target));
      dialog.close();
      notice(t("success"));
      await reloadConnections();
      await load();
    } catch (err) {
      document.getElementById("dialog-error").textContent = errorText(err);
    } finally {
      button.disabled = false;
    }
  };
  dialog.showModal();
  dialog.querySelector("input,textarea,select")?.focus();
}
function input(name, label, value = "", type = "text", required = false) {
  return `<label>${h(t(label))}<input name="${name}" value="${h(value)}" type="${type}" ${required ? "required" : ""} ${type === "password" ? 'autocomplete="new-password"' : ""}></label>`;
}
function check(name, label, value) {
  return `<label class="check"><input type="checkbox" name="${name}" ${value ? "checked" : ""}>${h(t(label))}</label>`;
}
function connectionDialog(c) {
  showDialog(
    c ? "edit" : "newConnection",
    input("name", "name", c?.name, "text", true) +
      input("endpoint", "endpoint", c?.endpoint) +
      input("region", "region", c?.region || "us-east-1", "text", true) +
      input("accessKey", "accessKey", c?.accessKey, "text", true) +
      input("secretKey", "secretKey", "", "password", !c) +
      input("sessionToken", "sessionToken", "", "password") +
      `<p class="hint">${h(t("secretHint"))}</p>` +
      check("pathStyle", "pathStyle", c?.pathStyle) +
      check("enabled", "enabled", c ? c.enabled : true) +
      input("grants", "grants", c?.grants.join(", ") || ""),
    async (f) => {
      await api(`/api/admin/credentials${c ? "/" + c.id : ""}`, {
        method: c ? "PUT" : "POST",
        body: {
          name: f.get("name"),
          endpoint: f.get("endpoint"),
          region: f.get("region"),
          accessKey: f.get("accessKey"),
          secretKey: f.get("secretKey"),
          sessionToken: f.get("sessionToken"),
          pathStyle: f.has("pathStyle"),
          enabled: f.has("enabled"),
          grants: f
            .get("grants")
            .split(",")
            .map((g) => g.trim())
            .filter(Boolean),
        },
      });
    },
  );
}
function userDialog(u) {
  showDialog(
    u ? "edit" : "newUser",
    (u ? `<p>${h(u.email)}</p>` : input("email", "email", "", "email", true)) +
      input("password", "password", "", "password", !u) +
      `<p class="hint">${h(t("passwordHint"))}</p><label>${h(t("role"))}<select name="role"><option value="USER">${h(t("user"))}</option><option value="ADMIN" ${u?.role === "ADMIN" ? "selected" : ""}>${h(t("admin"))}</option></select></label>` +
      check("enabled", "enabled", u ? u.enabled : true),
    async (f) => {
      await api(`/api/admin/users${u ? "/" + u.id : ""}`, {
        method: u ? "PUT" : "POST",
        body: {
          email: u?.email || f.get("email"),
          password: f.get("password"),
          role: f.get("role"),
          enabled: f.has("enabled"),
        },
      });
    },
  );
}
const configKinds = [
  "policy",
  "cors",
  "versioning",
  "lifecycle",
  "encryption",
  "tags",
  "public-access",
  "ownership",
];
const examples = {
  policy: { Policy: '{"Version":"2012-10-17","Statement":[]}' },
  cors: { CORSConfiguration: { CORSRules: [] } },
  versioning: { VersioningConfiguration: { Status: "Enabled" } },
  lifecycle: { LifecycleConfiguration: { Rules: [] } },
  encryption: {
    ServerSideEncryptionConfiguration: {
      Rules: [
        { ApplyServerSideEncryptionByDefault: { SSEAlgorithm: "AES256" } },
      ],
    },
  },
  tags: { Tagging: { TagSet: [] } },
  "public-access": {
    PublicAccessBlockConfiguration: {
      BlockPublicAcls: true,
      IgnorePublicAcls: true,
      BlockPublicPolicy: true,
      RestrictPublicBuckets: true,
    },
  },
  ownership: {
    OwnershipControls: { Rules: [{ ObjectOwnership: "BucketOwnerEnforced" }] },
  },
};
function configInput(kind, out) {
  const cleaned = { ...out };
  delete cleaned.ResultMetadata;
  delete cleaned.RequestCharged;
  delete cleaned.TransitionDefaultMinimumObjectSize;
  switch (kind) {
    case "policy":
      return { Policy: out.Policy };
    case "cors":
      return { CORSConfiguration: { CORSRules: out.CORSRules } };
    case "lifecycle":
      return { LifecycleConfiguration: { Rules: out.Rules } };
    case "tags":
      return { Tagging: { TagSet: out.TagSet } };
    case "versioning":
      return {
        VersioningConfiguration: {
          Status: out.Status,
          ...(out.MFADelete ? { MFADelete: out.MFADelete } : {}),
        },
      };
    default:
      return cleaned;
  }
}
function settingsDialog() {
  showDialog(
    "settings",
    `<p class="hint">${h(t("bucketConfigHint"))}</p><label>${h(t("kind"))}<select id="config-kind">${configKinds.map((k) => `<option value="${k}">${h(t(k))}</option>`).join("")}</select></label><div class="toolbar">${button("config-load", "load")}${button("config-delete", "delete", "", "danger")}</div><label>${h(t("document"))}<textarea name="document" id="config-document" spellcheck="false">${h(JSON.stringify(examples.policy, null, 2))}</textarea></label>`,
    async (f) => {
      let doc;
      try {
        doc = JSON.parse(f.get("document"));
      } catch {
        throw { message: t("invalidJSON"), code: "InvalidRequest" };
      }
      await api(
        objectQuery(bucket, {
          path: `config/${document.getElementById("config-kind").value}`,
        }),
        { method: "PUT", body: doc },
      );
    },
  );
}
async function reloadConnections() {
  if (!me) return;
  connections = await api("/api/credentials");
  me = await api("/api/me");
}
async function mutate(path, method, body) {
  await api(path, { method, ...(body === undefined ? {} : { body }) });
  notice(t("success"));
  await load();
}
async function action(a) {
  if (a === "close-dialog") {
    dialog.close();
    return;
  }
  if (uploadController && a !== "cancel-upload") return;
  if (a === "theme") {
    theme = theme === "dark" ? "light" : "dark";
    document.documentElement.dataset.theme = theme;
    try {
      localStorage.setItem("s3webui-theme", theme);
    } catch {}
    return;
  }
  if (a === "logout") {
    await api("/api/logout", { method: "POST" });
    me = null;
    connections = [];
    bucket = "";
    data = {};
    render();
    return;
  }
  if (a === "refresh") {
    await reloadConnections();
    await load();
    return;
  }
  if (a.startsWith("nav-")) {
    view = a.slice(4);
    resetPage();
    bucket = "";
    unfinished = false;
    await load();
    return;
  }
  if (a === "next") {
    let next;
    if (view === "audit") next = String(rows.at(-1)?.id);
    else if (unfinished)
      next = JSON.stringify({
        keyMarker: data.NextKeyMarker,
        uploadIdMarker: data.NextUploadIdMarker,
      });
    else if (versionMode)
      next = JSON.stringify({
        keyMarker: data.NextKeyMarker,
        versionMarker: data.NextVersionIdMarker,
      });
    else next = data.NextContinuationToken || data.ContinuationToken;
    if (next) {
      tokens.push(token);
      token = next;
      await load();
    }
    return;
  }
  if (a === "previous") {
    token = tokens.pop() || "";
    await load();
    return;
  }
  if (a === "back") {
    bucket = "";
    prefix = "";
    versionMode = false;
    unfinished = false;
    searchQuery = "";
    resetPage();
    await load();
    return;
  }
  if (a === "create-bucket") {
    showDialog(
      "createBucket",
      input("name", "bucketName", "", "text", true) +
        input(
          "region",
          "region",
          connections.find((c) => c.id === me.credentialId)?.region ||
            "us-east-1",
        ),
      async (f) =>
        api("/api/buckets", { method: "POST", body: Object.fromEntries(f) }),
      "create",
    );
    return;
  }
  if (a === "open-bucket") {
    showDialog(
      "openBucket",
      input("name", "bucketName", "", "text", true),
      async (f) => {
        bucket = f.get("name");
        prefix = "";
        versionMode = false;
        unfinished = false;
        searchQuery = "";
        resetPage();
      },
      "load",
    );
    return;
  }
  if (a === "folder") {
    showDialog(
      "folder",
      input("key", "folderName", prefix, "text", true),
      async (f) => {
        const k = f.get("key");
        if (!k.endsWith("/")) throw { code: "InvalidObjectKey" };
        const response = await fetch(objectQuery(bucket, { key: k }), {
          method: "PUT",
          body: new Uint8Array(0),
        });
        if (!response.ok) throw await response.json();
      },
      "create",
    );
    return;
  }
  if (a === "versions") {
    versionMode = !versionMode;
    unfinished = false;
    resetPage();
    await load();
    return;
  }
  if (a === "pending") {
    unfinished = !unfinished;
    versionMode = false;
    resetPage();
    await load();
    return;
  }
  if (a === "settings") {
    settingsDialog();
    return;
  }
  if (a === "config-load" || a === "config-delete") {
    const kind = document.getElementById("config-kind").value;
    if (
      a === "config-delete" &&
      !confirm(t("confirmDelete", { name: `${bucket} / ${t(kind)}` }))
    )
      return;
    try {
      const out = await api(objectQuery(bucket, { path: `config/${kind}` }), {
        method: a === "config-load" ? "GET" : "DELETE",
      });
      document.getElementById("config-document").value = JSON.stringify(
        a === "config-load" ? configInput(kind, out) : examples[kind],
        null,
        2,
      );
      document.getElementById("dialog-error").textContent = "";
    } catch (e) {
      document.getElementById("dialog-error").textContent = errorText(e);
    }
    return;
  }
  if (a === "new-credential") {
    connectionDialog();
    return;
  }
  if (a === "new-user") {
    userDialog();
    return;
  }
  if (a === "upload") {
    document.getElementById("files").click();
    return;
  }
  if (a === "cancel-upload") {
    uploadController?.abort();
    return;
  }
  if (a.startsWith("prefix-")) {
    prefix = data.CommonPrefixes[Number(a.slice(7))].Prefix;
    resetPage();
    await load();
    return;
  }
  const match = a.match(
    /^(bucket|delete-bucket|download|details|copy|move|delete-object|edit-credential|delete-credential|edit-user|delete-user|abort)-(\d+)$/,
  );
  if (!match) return;
  const [, op, index] = match;
  const row = rows[Number(index)];
  if (!row) return;
  if (op === "bucket") {
    bucket = row.Name;
    prefix = "";
    searchQuery = "";
    versionMode = false;
    unfinished = false;
    resetPage();
    await load();
    return;
  }
  if (op === "edit-credential") {
    connectionDialog(row);
    return;
  }
  if (op === "edit-user") {
    userDialog(row);
    return;
  }
  if (
    [
      "delete-bucket",
      "delete-object",
      "delete-credential",
      "delete-user",
      "abort",
    ].includes(op)
  ) {
    if (
      !confirm(
        t(op === "abort" ? "confirmAbort" : "confirmDelete", {
          name: row.Key || row.Name || row.name || row.email,
        }),
      )
    )
      return;
    if (op === "delete-bucket")
      await mutate(`/api/buckets/${encodeURIComponent(row.Name)}`, "DELETE");
    else if (op === "delete-credential" || op === "delete-user") {
      await api(
        `/api/admin/${op === "delete-user" ? "users" : "credentials"}/${row.id}`,
        { method: "DELETE" },
      );
      await reloadConnections();
      await load();
    } else if (op === "abort")
      await mutate(
        objectQuery(bucket, {
          path: "multipart/abort",
          key: row.Key,
          uploadId: row.UploadId,
        }),
        "DELETE",
      );
    else
      await mutate(
        objectQuery(bucket, {
          key: row.Key,
          ...(versionMode ? { versionId: row.VersionId } : {}),
        }),
        "DELETE",
      );
    return;
  }
  if (op === "download") {
    const a = document.createElement("a");
    a.href = objectQuery(bucket, {
      path: "objects/download",
      key: row.Key,
      ...(versionMode ? { versionId: row.VersionId } : {}),
    });
    a.download = "";
    document.body.append(a);
    a.click();
    a.remove();
    return;
  }
  if (op === "details") {
    const out = await api(
      objectQuery(bucket, {
        path: "objects/head",
        key: row.Key,
        ...(versionMode ? { versionId: row.VersionId } : {}),
      }),
    );
    dialog.innerHTML = `<h2 id="dialog-title">${h(t("details"))}</h2><pre>${h(JSON.stringify(out, null, 2))}</pre><div class="dialog-actions">${button("close-dialog", "cancel")}</div>`;
    dialog.showModal();
    return;
  }
  if (op === "copy" || op === "move") {
    showDialog(
      op,
      `<p class="hint">${h(t("copyHint"))}</p>` +
        input("destinationBucket", "destinationBucket", bucket, "text", true) +
        input("destination", "destination", row.Key, "text", true),
      async (f) => {
        if (op === "move" && !confirm(t("confirmMove", { name: row.Key })))
          throw { code: "InvalidRequest" };
        await api(objectQuery(bucket, { path: "objects/copy" }), {
          method: "POST",
          body: { key: row.Key, ...Object.fromEntries(f), move: op === "move" },
        });
      },
    );
  }
}
document.addEventListener("click", (e) => {
  const a = e.target.closest("[data-action]");
  if (a) action(a.dataset.action).catch((err) => notice(errorText(err)));
});
document.addEventListener("submit", async (e) => {
  if (e.target.id === "login-form") {
    e.preventDefault();
    const button = e.submitter;
    button.disabled = true;
    try {
      await api("/api/login", {
        method: "POST",
        body: Object.fromEntries(new FormData(e.target)),
      });
      me = await api("/api/me");
      await reloadConnections();
      await load();
    } catch (err) {
      document.getElementById("login-error").textContent = errorText(err);
    } finally {
      button.disabled = false;
    }
  }
  if (e.target.id === "search-form") {
    e.preventDefault();
    if (uploadController) return;
    const f = new FormData(e.target);
    prefix = f.get("prefix");
    searchQuery = f.get("q") || "";
    recursive = f.has("recursive");
    resetPage();
    await load();
  }
});
document.addEventListener("change", async (e) => {
  try {
    if (e.target.id === "language") {
      if (uploadController) {
        e.target.value = locale;
        return;
      }
      locale = e.target.value;
      try {
        localStorage.setItem("s3webui-locale", locale);
      } catch {}
      dialog.close();
      document.getElementById("notice").classList.remove("visible");
      render();
    }
    if (e.target.id === "connection" && e.target.value) {
      if (uploadController) return;
      await api("/api/credentials/select", {
        method: "POST",
        body: { id: Number(e.target.value) },
      });
      me.credentialId = Number(e.target.value);
      view = "buckets";
      bucket = "";
      prefix = "";
      versionMode = false;
      unfinished = false;
      resetPage();
      await load();
      notice(t("connectionChanged"));
    }
    if (e.target.id === "config-kind") {
      document.getElementById("config-document").value = JSON.stringify(
        examples[e.target.value],
        null,
        2,
      );
      document.getElementById("dialog-error").textContent = "";
    }
    if (e.target.id === "files") {
      const files = [...e.target.files];
      if (
        !files.length ||
        uploadController ||
        !confirm(t("confirmOverwrite", { count: files.length }))
      )
        return;
      uploadController = new AbortController();
      setUploadBusy();
      const b = bucket,
        p = prefix,
        credentialID = me.credentialId;
      const status = document.getElementById("upload-status");
      try {
        for (const file of files) {
          const progress = (loaded) => {
            if (status.isConnected)
              status.innerHTML = `<p>${h(t("uploading", { name: file.name, percent: Math.min(100, Math.round((loaded / Math.max(1, file.size)) * 100)) }))}</p><progress max="${file.size || 1}" value="${loaded}"></progress>${button("cancel-upload", "cancelUpload", "", "small")}`;
          };
          progress(0);
          await uploadFile(
            b,
            p + file.name,
            file,
            api,
            uploadController.signal,
            progress,
            credentialID,
          );
        }
        notice(t("success"));
      } catch (err) {
        notice(
          err.name === "AbortError" ? t("uploadCancelled") : errorText(err),
        );
      } finally {
        uploadController = null;
        await load();
      }
    }
  } catch (err) {
    notice(errorText(err));
  }
});
async function start() {
  try {
    meta = await api("/api/meta");
    try {
      me = await api("/api/me");
      await reloadConnections();
    } catch {
      me = null;
    }
    await load();
  } catch (err) {
    notice(errorText(err));
    render();
  }
}
start();

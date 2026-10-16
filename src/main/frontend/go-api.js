// SPDX-License-Identifier: AGPL-3.0-only
// Keep the author's controllers and payloads; adapt their transport to the Go API.
import { translate as t } from "./i18n.js";
const response = (body, status = 200) =>
  new Response(body === null ? null : JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
const raw = (url, opts = {}) =>
  fetch(url, { ...opts, credentials: "same-origin" });
async function json(url, opts) {
  const r = await raw(url, opts);
  if (!r.ok) throw { response: r };
  return r.status === 204 ? null : r.json();
}
let selected = null;
const asGrant = (s) =>
  s === "*"
    ? { grantType: "ALL_AUTHENTICATED", grantValue: null }
    : s.startsWith("role:")
      ? { grantType: "ROLE", grantValue: s.slice(5) }
      : { grantType: "USER", grantValue: s };
const credential = (c) => ({
  ...c,
  endpointUrl: c.endpoint,
  insecureSkipTlsVerify: false,
  builtIn: false,
  grants: c.grants.map(asGrant),
});
async function allBuckets() {
  const out = [];
  let token;
  do {
    const page = await json(
      "/api/buckets?" + new URLSearchParams(token ? { token } : {}),
    );
    for (const b of page.Buckets ?? [])
      out.push({ name: b.Name, creationDate: b.CreationDate });
    token = page.ContinuationToken;
  } while (token);
  return out;
}
async function invoke(url, options = {}) {
  const u = new URL(url, location.origin),
    method = (options.method ?? "GET").toUpperCase();
  let path = u.pathname;
  let body = options.body;
  const init = { ...options, headers: new Headers(options.headers ?? {}) };
  const send = (p = path, data = body, m = method) =>
    raw(p, {
      ...init,
      method: m,
      ...(data === undefined
        ? {}
        : {
            body:
              typeof data === "string" ||
              data instanceof Blob ||
              data instanceof FormData
                ? data
                : JSON.stringify(data),
          }),
    });
  if (selected && path.startsWith("/api/buckets/"))
    init.headers.set("X-S3-Credential-ID", selected);
  if (path === "/api/s3/session") {
    if (method === "GET") {
      const me = await json("/api/me");
      const credentials = (await json("/api/credentials")).map((c) => ({
        ...credential(c),
        id: String(c.id),
      }));
      selected = me.credentialId === null ? null : String(me.credentialId);
      window.activeCredentialId = selected;
      return response({
        selectionRequired:
          !selected && (credentials.length > 0 || me.role !== "ADMIN"),
        activeCredentialId: selected,
        activeCredentialName:
          credentials.find((c) => c.id === selected)?.name ?? null,
        allowOwnCredentials: false,
        allowUsersToRevealKeys: false,
        credentials,
      });
    }
    const v = JSON.parse(body);
    return send(
      "/api/credentials/select",
      { id: Number(v.credentialId) },
      "POST",
    );
  }
  if (path === "/api/buckets") {
    if (method === "GET") return response(await allBuckets());
    if (method === "POST") {
      init.headers.set("Content-Type", "application/json");
      return send("/api/buckets", { name: u.searchParams.get("name") }, "POST");
    }
  }
  if (path === "/api/search")
    return send("/api/ui/search" + u.search, undefined, "GET");
  if (path === "/api/history") {
    let before = "";
    const out = [];
    do {
      const page = await json(
        "/api/audit" + (before ? "?before=" + before : ""),
      );
      out.push(
        ...page.map((e) => ({
          timestamp: e.date,
          user: e.actor,
          action: e.action,
          resourceType: e.bucket ? "OBJECT" : "ADMIN",
          bucket: e.bucket,
          key: e.key,
          details:
            e.code === "OK" ? "" : t("Failed: {0}").replace("{0}", e.code),
        })),
      );
      before = page.length === 100 ? String(page.at(-1).id) : "";
    } while (before);
    return response(out);
  }
  if (path === "/api/admin/settings") {
    if (method !== "GET") {
      const v = JSON.parse(body);
      if (v.allowUserSuppliedCredentials || v.allowUsersToRevealKeys)
        return response({ message: t("Unavailable in the Go backend") }, 501);
    }
    return response({
      allowUserSuppliedCredentials: false,
      allowUsersToRevealKeys: false,
    });
  }
  if (path === "/api/admin/iam/capabilities")
    return response({
      available: false,
      reason: t("IAM management is not available for this S3 key."),
      provider: "S3",
      users: false,
      groups: false,
      policies: false,
    });
  if (/^\/api\/admin\/credentials(?:\/\d+)?$/.test(path)) {
    if (method === "GET") return response((await json(path)).map(credential));
    if (method === "POST" || method === "PUT") {
      const v = JSON.parse(body);
      if (v.insecureSkipTlsVerify)
        return response({ message: t("Unavailable in the Go backend") }, 400);
      const grants = v.grants.map((g) =>
        g.grantType === "ALL_AUTHENTICATED"
          ? "*"
          : g.grantType === "ROLE"
            ? "role:" + g.grantValue
            : g.grantValue,
      );
      return send(path, {
        name: v.name,
        endpoint: v.endpointUrl,
        region: v.region,
        accessKey: v.accessKey,
        secretKey: v.secretKey,
        pathStyle: Boolean(v.endpointUrl),
        enabled: v.enabled,
        grants,
      });
    }
  }
  if (/^\/api\/admin\/users(?:\/\d+)?(?:\/password)?$/.test(path)) {
    if (method === "GET")
      return response(
        (await json(path)).map((u) => ({
          ...u,
          provider: "LOCAL",
          lastLoginAt: null,
        })),
      );
    if (method === "POST" && !path.endsWith("/password"))
      return send(path, { ...JSON.parse(body), enabled: true });
    if (method === "PUT" || path.endsWith("/password")) {
      const id = Number(path.split("/")[4]);
      const current = (await json("/api/admin/users")).find((u) => u.id === id);
      const changes = JSON.parse(body);
      return send(
        "/api/admin/users/" + id,
        {
          email: current.email,
          role: changes.role ?? current.role,
          enabled: changes.enabled ?? current.enabled,
          password: changes.password ?? "",
        },
        "PUT",
      );
    }
  }
  if (path.endsWith("/objects/upload") && method === "POST") {
    const file = body.get("file"),
      prefix = u.searchParams.get("prefix") ?? "";
    init.headers.set("Content-Type", file.type || "application/octet-stream");
    return send(
      path.replace("/objects/upload", "/objects") +
        "?" +
        new URLSearchParams({ key: prefix + file.name }),
      file,
      "PUT",
    );
  }
  if (path.endsWith("/objects/rename")) {
    init.headers.set("Content-Type", "application/json");
    return send(
      path.replace("/rename", "/copy"),
      {
        key: u.searchParams.get("oldKey"),
        destination: u.searchParams.get("newKey"),
        move: true,
      },
      "POST",
    );
  }
  if (/\/multipart\/(initiate|part|complete)$/.test(path)) {
    if (path.endsWith("/complete")) {
      const v = JSON.parse(body);
      u.searchParams.set("uploadId", v.uploadId);
      return send(path + "?" + u.searchParams, {
        parts: v.parts.map((p) => ({ PartNumber: p.partNumber, ETag: p.eTag })),
      });
    }
    const r = await send(path + u.search);
    if (!r.ok) return r;
    const v = await r.json();
    return response(
      path.endsWith("/initiate") ? { uploadId: v.UploadId } : { eTag: v.ETag },
    );
  }
  if (/\/buckets\/[^/]+\/(policy|cors)$/.test(path)) {
    const kind = path.split("/").at(-1),
      target = path.replace("/" + kind, "/config/" + kind);
    if (method === "GET") {
      const r = await raw(target);
      if (r.status === 404)
        return response(
          kind === "policy" ? { policy: null } : { CORSRules: [] },
        );
      if (!r.ok) return r;
      const v = await r.json();
      return response(
        kind === "policy"
          ? { policy: v.Policy }
          : { CORSRules: v.CORSRules ?? [] },
      );
    }
    if (method === "PUT")
      return send(
        target,
        kind === "policy"
          ? { Policy: body }
          : { CORSConfiguration: JSON.parse(body) },
      );
    return send(target, undefined, method);
  }
  return send(path + u.search);
}
export async function goFetch(url, options) {
  try {
    const r = await invoke(url, options);
    if (!r.ok) {
      if (r.status === 401) {
        location.assign("/login");
        return r;
      }
      try {
        const v = await r.clone().json();
        return response(
          {
            ...v,
            message:
              v.message ?? `${t("Request failed")} (${v.code ?? r.status})`,
          },
          r.status,
        );
      } catch {}
    }
    return r;
  } catch (e) {
    if (e.response) return e.response;
    throw e;
  }
}

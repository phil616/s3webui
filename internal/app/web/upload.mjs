// SPDX-License-Identifier: AGPL-3.0-only
export function uploadPlan(size) {
  if (!Number.isSafeInteger(size) || size < 0 || size > 10000 * 64 * 1024 ** 2)
    throw Object.assign(new Error("EntityTooLarge"), {
      code: "EntityTooLarge",
    });
  const partSize = Math.max(
    8 * 1024 ** 2,
    Math.ceil(size / 10000 / 1024 ** 2) * 1024 ** 2,
  );
  return {
    multipart: size > 16 * 1024 ** 2,
    partSize,
    count: Math.ceil(size / partSize),
  };
}
export function objectQuery(bucket, params = {}) {
  return `/api/buckets/${encodeURIComponent(bucket)}/${params.path ?? "objects"}?${new URLSearchParams(Object.entries(params).filter(([k, v]) => k !== "path" && v !== undefined && v !== null))}`;
}
export function escapeHTML(value) {
  return String(value ?? "").replace(
    /[&<>"']/g,
    (c) =>
      ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[
        c
      ],
  );
}
export function xhrUpload(url, body, signal, onProgress, headers = {}) {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open("PUT", url);
    for (const [name, value] of Object.entries(headers))
      xhr.setRequestHeader(name, value);
    xhr.setRequestHeader(
      "Content-Type",
      body.type || "application/octet-stream",
    );
    const abort = () => xhr.abort();
    signal.addEventListener("abort", abort, { once: true });
    xhr.upload.onprogress = (e) => {
      if (e.lengthComputable) onProgress(e.loaded);
    };
    xhr.onloadend = () => signal.removeEventListener("abort", abort);
    xhr.onload = () => {
      let data;
      try {
        data = JSON.parse(xhr.responseText);
      } catch {}
      if (xhr.status >= 200 && xhr.status < 300) resolve(data);
      else
        reject(
          Object.assign(new Error(data?.code || String(xhr.status)), {
            code: data?.code || String(xhr.status),
          }),
        );
    };
    xhr.onerror = () =>
      reject(
        Object.assign(new Error("NetworkError"), { code: "NetworkError" }),
      );
    xhr.onabort = () => reject(new DOMException("Aborted", "AbortError"));
    if (signal.aborted) {
      reject(new DOMException("Aborted", "AbortError"));
      return;
    }
    xhr.send(body);
  });
}
export async function uploadFile(
  bucket,
  key,
  file,
  api,
  signal,
  onProgress,
  credentialID,
) {
  const plan = uploadPlan(file.size);
  const base = { key };
  const headers = credentialID
    ? { "X-S3-Credential-ID": String(credentialID) }
    : {};
  if (!plan.multipart) {
    await xhrUpload(
      objectQuery(bucket, base),
      file,
      signal,
      onProgress,
      headers,
    );
    return;
  }
  const created = await api(
    objectQuery(bucket, {
      ...base,
      path: "multipart/initiate",
      contentType: file.type || "application/octet-stream",
    }),
    { method: "POST", signal, headers },
  );
  const params = { ...base, uploadId: created.UploadId };
  let completed = false;
  try {
    const parts = [];
    for (let number = 1; number <= plan.count; number++) {
      if (signal.aborted) throw new DOMException("Aborted", "AbortError");
      const offset = (number - 1) * plan.partSize;
      const part = await xhrUpload(
        objectQuery(bucket, {
          ...params,
          path: "multipart/part",
          partNumber: number,
        }),
        file.slice(offset, offset + plan.partSize),
        signal,
        (loaded) => onProgress(offset + loaded),
        headers,
      );
      parts.push({ PartNumber: number, ETag: part.ETag });
    }
    await api(objectQuery(bucket, { ...params, path: "multipart/complete" }), {
      method: "POST",
      body: { parts },
      signal,
      headers,
    });
    completed = true;
  } finally {
    if (!completed) {
      try {
        await api(objectQuery(bucket, { ...params, path: "multipart/abort" }), {
          method: "DELETE",
          headers,
        });
      } catch {
        /* Upload remains visible in unfinished uploads for manual cleanup. */
      }
    }
  }
}

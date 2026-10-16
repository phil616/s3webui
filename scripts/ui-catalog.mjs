// SPDX-License-Identifier: AGPL-3.0-only
import fs from "node:fs";
import path from "node:path";
import { parse as htmlParse } from "parse5";
import { parse as jsParse } from "@babel/parser";
import traverseModule from "@babel/traverse";
const traverse = traverseModule.default ?? traverseModule;
const phrases = new Set();
export const normalize = (s) => s.replace(/\s+/g, " ").trim();
export function add(s) {
  s = normalize(s);
  if (
    /[A-Za-z]/.test(s) &&
    !/^https?:|^\/|^[.#@]|^data-/.test(s) &&
    s.length < 700
  )
    phrases.add(s);
}
export function htmlPhrases(source) {
  const doc = htmlParse(source);
  function walk(n) {
    if (n.tagName === "script") {
      const code = n.childNodes?.map((n) => n.value ?? "").join("") ?? "";
      jsPhrases(code.replace(/\/\*\[\[.*?\]\]\*\//gs, ""));
      return;
    }
    if (n.tagName === "style") return;
    if (n.nodeName === "#text") add(n.value);
    for (const a of n.attrs ?? []) {
      if (["title", "aria-label", "placeholder", "alt"].includes(a.name))
        add(a.value);
    }
    for (const c of n.childNodes ?? []) walk(c);
  }
  walk(doc);
}
export function human(s) {
  return (
    /[A-Za-z]/.test(s) &&
    ((/\s/.test(s) &&
      !/[{};=\\]/.test(s.replace(/\{\d+\}/g, "")) &&
      !/^https?:|^\//.test(s)) ||
      [
        "Done",
        "Remove",
        "Cancel",
        "Close",
        "Save",
        "Enabled",
        "Disabled",
        "Loading…",
        "Upload",
        "Copy",
        "Move",
        "Delete",
        "Previous",
        "Next",
        "Anonymous",
        "FAILED",
      ].includes(s))
  );
}
export function jsPhrases(source) {
  let ast;
  try {
    ast = jsParse(source, { sourceType: "unambiguous" });
  } catch {
    return;
  }
  traverse(ast, {
    StringLiteral(p) {
      const s = p.node.value;
      if (s.includes("<") && s.includes(">")) {
        htmlPhrases(s);
        return;
      }
      if (human(s)) add(s);
    },
    TemplateLiteral(p) {
      const s = p.node.quasis
        .map(
          (q, i) =>
            (q.value.cooked ?? q.value.raw) +
            (i < p.node.expressions.length ? `{${i}}` : ""),
        )
        .join("");
      if (s.includes("<") && s.includes(">")) htmlPhrases(s);
      else if (human(s)) add(s);
    },
  });
}
export function files(root) {
  return fs
    .readdirSync(root, { withFileTypes: true })
    .flatMap((d) =>
      d.isDirectory()
        ? files(path.join(root, d.name))
        : [path.join(root, d.name)],
    );
}
if (process.argv[1]?.endsWith("ui-catalog.mjs")) {
  for (const file of files("src/main/resources/templates"))
    if (file.endsWith(".html")) htmlPhrases(fs.readFileSync(file, "utf8"));
  for (const file of files("src/main/frontend"))
    if (/\.(js|mjs)$/.test(file)) jsPhrases(fs.readFileSync(file, "utf8"));
  fs.mkdirSync(".local", { recursive: true });
  fs.writeFileSync(
    ".local/ui-phrases.json",
    JSON.stringify([...phrases].sort(), null, 2),
  );
  console.log(phrases.size);
}

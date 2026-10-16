// SPDX-License-Identifier: AGPL-3.0-only
import zh from "../resources/locales/zh.json";
export const normalize = (value) => String(value).replace(/\s+/g, " ").trim();
export const locale = () =>
  document.documentElement.lang.startsWith("zh") ? "zh" : "en";
export function translate(value) {
  if (locale() !== "zh") return value;
  const key = normalize(value),
    replacement = zh[key];
  return replacement === undefined
    ? value
    : value.match(/^\s*/)[0] + replacement + value.match(/\s*$/)[0];
}
export function translateHTML(value) {
  if (locale() !== "zh") return value;
  const template = document.createElement("template");
  template.innerHTML = value;
  const walker = document.createTreeWalker(
    template.content,
    NodeFilter.SHOW_TEXT | NodeFilter.SHOW_ELEMENT,
  );
  while (walker.nextNode()) {
    const node = walker.currentNode;
    if (
      node.nodeType === Node.TEXT_NODE &&
      node.parentElement?.tagName !== "SCRIPT"
    )
      node.textContent = translate(node.textContent);
    else if (node.nodeType === Node.ELEMENT_NODE)
      for (const name of ["title", "aria-label", "placeholder", "alt"])
        if (node.hasAttribute(name))
          node.setAttribute(name, translate(node.getAttribute(name)));
  }
  return template.innerHTML;
}
export function template(strings, values) {
  const pattern = strings
    .map((s, i) => s + (i < values.length ? `{${i}}` : ""))
    .join("");
  const result = /<[a-z][\s>]/i.test(pattern)
    ? translateHTML(pattern)
    : translate(pattern);
  return result.replace(/\{(\d+)\}/g, (_, i) =>
    String(values[Number(i)] ?? ""),
  );
}
window.__s3i18n = { text: translate, html: translateHTML, template };
document.addEventListener("change", (event) => {
  if (!event.target.matches("[data-language]")) return;
  const language = event.target.value === "zh" ? "zh" : "en";
  document.cookie = `s3webui-language=${language}; Path=/; SameSite=Lax`;
  try {
    localStorage.setItem("s3webui-locale", language);
  } catch {}
  location.reload();
});

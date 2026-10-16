// SPDX-License-Identifier: AGPL-3.0-only
import fs from "node:fs";
import path from "node:path";
import { execFileSync } from "node:child_process";
import { build } from "esbuild";
import { parse } from "@babel/parser";
import traverseModule from "@babel/traverse";
import generateModule from "@babel/generator";
import zh from "../src/main/resources/locales/zh.json" with { type: "json" };
const traverse = traverseModule.default ?? traverseModule;
const generate = generateModule.default ?? generateModule;
const normalize = (s) => s.replace(/\s+/g, " ").trim();
function literal(value) {
  return { type: "StringLiteral", value };
}
function call(method, args) {
  return {
    type: "CallExpression",
    callee: {
      type: "MemberExpression",
      object: {
        type: "MemberExpression",
        object: { type: "Identifier", name: "window" },
        property: { type: "Identifier", name: "__s3i18n" },
        computed: false,
      },
      property: { type: "Identifier", name: method },
      computed: false,
    },
    arguments: args,
  };
}
export function localizeScript(source) {
  const ast = parse(source, { sourceType: "unambiguous" });
  traverse(ast, {
    CallExpression: {
      exit(p) {
        const c = p.node.callee;
        if (
          c.type !== "MemberExpression" ||
          c.computed ||
          ![
            "toLocaleString",
            "toLocaleDateString",
            "toLocaleTimeString",
          ].includes(c.property.name)
        )
          return;
        const a = p.node.arguments;
        if (a.length === 0 || a[0]?.type === "ArrayExpression") {
          const language = {
            type: "MemberExpression",
            object: {
              type: "MemberExpression",
              object: { type: "Identifier", name: "document" },
              property: { type: "Identifier", name: "documentElement" },
              computed: false,
            },
            property: { type: "Identifier", name: "lang" },
            computed: false,
          };
          if (a.length === 0) a.push(language);
          else a[0] = language;
        }
      },
    },
    StringLiteral(p) {
      const value = p.node.value;
      if (
        p.parent.type.startsWith("Import") ||
        p.parent.type.startsWith("Export") ||
        (p.parent.type === "ObjectProperty" && p.key === "key") ||
        p.parent.type === "Directive"
      )
        return;
      const html = /<[a-z][\s>]/i.test(value);
      if (
        html ||
        (zh[normalize(value)] !== undefined &&
          zh[normalize(value)] !== normalize(value))
      ) {
        p.replaceWith(call(html ? "html" : "text", [literal(value)]));
        p.skip();
      }
    },
    TemplateLiteral: {
      exit(p) {
        if (p.parent.type === "TaggedTemplateExpression") return;
        const strings = p.node.quasis.map((q) => q.value.cooked ?? q.value.raw);
        const pattern = strings
          .map((s, i) => s + (i < p.node.expressions.length ? `{${i}}` : ""))
          .join("");
        if (
          !/<[a-z][\s>]/i.test(pattern) &&
          zh[normalize(pattern)] === undefined
        )
          return;
        p.replaceWith(
          call("template", [
            { type: "ArrayExpression", elements: strings.map(literal) },
            { type: "ArrayExpression", elements: p.node.expressions },
          ]),
        );
        p.skip();
      },
    },
  });
  return generate(ast, { comments: true }).code;
}
function files(root) {
  return fs
    .readdirSync(root, { withFileTypes: true })
    .flatMap((d) =>
      d.isDirectory()
        ? files(path.join(root, d.name))
        : [path.join(root, d.name)],
    );
}
fs.mkdirSync("src/main/resources/rendered", { recursive: true });
for (const file of files("src/main/resources/templates")) {
  if (!file.endsWith(".html")) continue;
  const source = fs.readFileSync(file, "utf8");
  const result = source.replace(
    /(<script\b[^>]*>)([\s\S]*?)(<\/script>)/gi,
    (_, open, body, close) =>
      body.trim() ? open + localizeScript(body) + close : open + body + close,
  );
  const output = file.replace(/templates([/\\])/, "rendered$1");
  fs.mkdirSync(path.dirname(output), { recursive: true });
  fs.writeFileSync(output, result);
}
fs.mkdirSync("src/main/resources/static/css", { recursive: true });
execFileSync(
  process.execPath,
  [
    "node_modules/@tailwindcss/cli/dist/index.mjs",
    "-i",
    "src/main/frontend/s3webui.css",
    "-o",
    "src/main/resources/static/css/s3webui.css",
    "--minify",
  ],
  { stdio: "inherit" },
);
await build({
  entryPoints: ["src/main/frontend/s3webui.js"],
  bundle: true,
  minify: true,
  format: "iife",
  outfile: "src/main/resources/static/js/s3webui.js",
  plugins: [
    {
      name: "original-ui-i18n",
      setup(b) {
        b.onLoad(
          { filter: /src[\\/]main[\\/]frontend[\\/].*\.(js|mjs)$/ },
          (args) => {
            const source = fs.readFileSync(args.path, "utf8");
            if (/(?:i18n|go-api|upload-core)\.(?:js|mjs)$/.test(args.path))
              return { contents: source, loader: "js" };
            return { contents: localizeScript(source), loader: "js" };
          },
        );
      },
    },
  ],
});
console.log(
  "Built original templates, Tailwind stylesheet, Alpine/Lucide/CodeMirror bundle with i18n.",
);
const lock = JSON.parse(fs.readFileSync("package-lock.json", "utf8"));
const notices = [];
for (const [directory, pkg] of Object.entries(lock.packages ?? {})) {
  if (
    !directory.startsWith("node_modules/") ||
    pkg.dev ||
    !fs.existsSync(directory)
  )
    continue;
  for (const name of fs.readdirSync(directory))
    if (
      /^(licen[sc]e|copying|notice)(\.|$)/i.test(name) &&
      fs.statSync(path.join(directory, name)).isFile()
    )
      notices.push(
        `===== ${directory.replace(/^node_modules\//, "")} ${pkg.version} / ${name} =====\n` +
          fs.readFileSync(path.join(directory, name), "utf8"),
      );
}
fs.mkdirSync("src/main/resources/static/licenses", { recursive: true });
fs.writeFileSync(
  "src/main/resources/static/licenses/third-party.txt",
  notices.join("\n\n"),
);

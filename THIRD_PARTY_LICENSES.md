# Third-party fonts

The Go downstream uses the original author's templates, styles, logo, fonts and browser libraries.
Their existing notices are preserved. Full installed production JavaScript license texts are copied
by the build to `/licenses/third-party.txt`, served with the application and included in source archives.

## Active Go dependencies

Pinned versions and transitive dependencies: `go.mod` and `go.sum`. Container source archives include
the vendored modules and their original LICENSE/NOTICE files.

| Component | License | Source |
| --- | --- | --- |
| AWS SDK for Go v2 | Apache-2.0 | https://github.com/aws/aws-sdk-go-v2 |
| Smithy Go | Apache-2.0 | https://github.com/aws/smithy-go |
| Expr | MIT | https://github.com/expr-lang/expr |
| pgx / pgpassfile / pgservicefile / puddle | MIT | https://github.com/jackc |
| Go x/crypto, x/net, x/sync, x/text | BSD-3-Clause | https://go.googlesource.com/ |

Browser runtime: Alpine.js (MIT), CodeMirror and its language/editor packages (MIT), and Lucide (ISC).
Tailwind CSS (MIT) compiles the unchanged original stylesheet. Babel and parse5 (MIT) provide build-time
localization; they are not a second frontend framework. node-postgres (MIT) is used only in browser tests.
Playwright is used only for development/browser checks (Apache-2.0; https://github.com/microsoft/playwright).

S3WEBUI bundles the following fonts so the user interface does not depend on an external font service.

## Inter 4.1

- Copyright: The Inter Project Authors
- Source: https://github.com/rsms/inter/releases/tag/v4.1
- License: SIL Open Font License 1.1
- Bundled files: `InterVariable.woff2`, `OFL.txt`

## JetBrains Mono 2.304

- Copyright: JetBrains s.r.o.
- Source: https://github.com/JetBrains/JetBrainsMono/releases/tag/v2.304
- License: SIL Open Font License 1.1
- Bundled files: `JetBrainsMono-Regular.woff2`, `OFL.txt`


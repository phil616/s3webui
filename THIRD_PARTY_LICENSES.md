# Third-party fonts

The Go downstream serves system fonts and does not bundle the legacy font or JavaScript assets
in its active interface. The original files and their notices below are retained as upstream reference.

## Active Go dependencies

Pinned versions and transitive dependencies: `go.mod` and `go.sum`. Container source archives include
the vendored modules and their original LICENSE/NOTICE files.

| Component | License | Source |
| --- | --- | --- |
| AWS SDK for Go v2 | Apache-2.0 | https://github.com/aws/aws-sdk-go-v2 |
| Smithy Go | Apache-2.0 | https://github.com/aws/smithy-go |
| pgx / pgpassfile / pgservicefile / puddle | MIT | https://github.com/jackc |
| Go x/crypto, x/sync, x/text | BSD-3-Clause | https://go.googlesource.com/ |

The Go interface has no third-party browser runtime dependencies.
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


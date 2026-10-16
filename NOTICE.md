# Attribution and downstream changes

S3 Web UI — Copyright (C) 2026 Jean-Fabian Wenisch / wenisch.tech.
Original project: https://github.com/wenisch-tech/s3webui
Fork baseline: `a820d5945127cfc60b48e3c03c5eb1e54e09fc17`.

Downstream changes, 2026-10-16: Go backend, PostgreSQL application persistence,
Chinese/English browser interface, S3 semantics fixes, deployment and manual upstream review tooling.
These changes are maintained downstream; they are not represented as endorsed by the original author.
Frontend correction, 2026-10-16: restore the original author's actual templates, CSS, logo,
Alpine/Lucide/CodeMirror controllers and interactions. Chinese/English localization and Go transport
adapters extend those sources; the separately rewritten interface has been removed.
The original authors retain their copyright. Existing copyright and third-party notices are retained.

This modified work remains licensed under GNU AGPL version 3; see LICENSE for its full terms,
including sections 4–6 and 13. No warranty is provided. Redistribution is permitted under that license.
Replacing the implementation language does not remove the licensing obligations of this modified work.

Every running version offers a visible source-code link, including on the sign-in screen.
Container builds include their corresponding source, build scripts and Go dependencies at `/source`.
Native deployments must configure SOURCE_URL to a freely downloadable source archive of their exact
deployed version, or SOURCE_ARCHIVE to an equivalent local archive. Regenerate it for every modified
deployment; linking only to the original upstream repository is insufficient.

This notice supplements, and does not replace or restrict, LICENSE.

// SPDX-License-Identifier: AGPL-3.0-only
package resources

import "embed"

// The original author's templates, fonts, logo and built frontend are the active UI.
//
//go:embed templates static locales rendered
var Files embed.FS

// SPDX-License-Identifier: AGPL-3.0-only
package app

import (
	_ "embed"
	"log/slog"
)

//go:embed LICENSE.txt
var licenseText string

func logAuditError(err error) { slog.Error("audit write failed after operation", "error", err) }

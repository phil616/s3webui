# SPDX-License-Identifier: AGPL-3.0-only
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][ValidatePattern('^[0-9a-fA-F]{40}$')][string]$Target,
    [string]$OutputDirectory = 'docs/upstream/reviews'
)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
$repoRoot = Split-Path -Parent $PSScriptRoot
Push-Location $repoRoot
try {
    $state = Get-Content -LiteralPath 'docs/upstream/state.json' -Raw | ConvertFrom-Json
    $baseline = [string]$state.lastReviewed
    if ($baseline -notmatch '^[0-9a-f]{40}$') { throw 'Invalid lastReviewed SHA' }
    if ($state.repository -ne 'https://github.com/wenisch-tech/s3webui') { throw 'Review repository URL policy before changing upstream' }
    $Target = $Target.ToLowerInvariant()
    # Only FETCH_HEAD and the Git object database change. No checkout, merge, apply, or push.
    git fetch --no-tags --no-recurse-submodules $state.repository $Target
    if ($LASTEXITCODE -ne 0) { throw 'Fetch failed' }
    git cat-file -e "$baseline^{commit}"
    if ($LASTEXITCODE -ne 0) { throw 'Reviewed baseline is unavailable locally' }
    $resolved = git rev-parse 'FETCH_HEAD^{commit}'
    if ($LASTEXITCODE -ne 0 -or $resolved -ne $Target) { throw 'Fetched SHA differs from requested SHA' }
    git merge-base --is-ancestor $baseline $Target
    if ($LASTEXITCODE -ne 0) { throw 'Target must descend from lastReviewed; review diverged histories separately' }
    $reportDir = Join-Path (Join-Path $repoRoot $OutputDirectory) $Target
    New-Item -ItemType Directory -Path $reportDir -Force | Out-Null
    $commits = git log --format='%H %ad %s' --date=short "$baseline..$Target"
    if ($LASTEXITCODE -ne 0) { throw 'Cannot list commits' }
    $paths = git diff --name-status $baseline $Target
    if ($LASTEXITCODE -ne 0) { throw 'Cannot compare commits' }
    $license = git show "${Target}:LICENSE"
    if ($LASTEXITCODE -ne 0) { throw 'Upstream LICENSE missing: stop integration and review licensing' }
    $licenseDiff = git diff $baseline $Target -- LICENSE THIRD_PARTY_LICENSES.md
    if ($LASTEXITCODE -ne 0) { throw 'Cannot compare license notices' }
    # Native --output preserves patch encoding and newlines.
    git diff --binary "--output=$reportDir/changes.patch" $baseline $Target
    if ($LASTEXITCODE -ne 0) { throw 'Cannot produce patch' }
    $license | Set-Content -LiteralPath (Join-Path $reportDir 'upstream-LICENSE.txt') -Encoding utf8
    $report = @(
        '# Upstream review (not applied)', '', "Repository: $($state.repository)",
        "Reviewed base: $baseline", "Target: $Target", '',
        '## Commits', '```text', ($commits -join "`n"), '```', '',
        '## Changed paths', '```text', ($paths -join "`n"), '```', '',
        '## License/notice changes - review before adoption', '```diff', ($licenseDiff -join "`n"), '```', '',
        '## Maintainer decisions (complete manually)', '',
        '| Commit / paths | Adopt / port / skip / defer | Reason | Downstream commit / tests |',
        '| --- | --- | --- | --- |', '| TODO | TODO | TODO | TODO |', '',
        'Review Java security fixes for equivalent Go risks even when code cannot be reused.',
        'Review frontend changes for both languages and browser compatibility.',
        'Do not apply the entire patch blindly. No application changes have been made.',
        'After review, update docs/upstream/state.json through a reviewed PR; preserve attribution.'
    )
    $report | Set-Content -LiteralPath (Join-Path $reportDir 'REVIEW.md') -Encoding utf8
    Write-Output "Review generated: $reportDir/REVIEW.md"
} finally { Pop-Location }

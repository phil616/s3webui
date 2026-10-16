# SPDX-License-Identifier: AGPL-3.0-only
# Builds from an immutable Git commit, including vendored corresponding source.
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][ValidatePattern('^v[0-9]+\.[0-9]+\.[0-9]+$')][string]$Version,
    [string]$Revision = 'HEAD'
)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
$repoRoot = Split-Path -Parent $PSScriptRoot
$savedEnvironment = @{}
foreach ($envName in @('GOOS', 'GOARCH', 'GOAMD64', 'CGO_ENABLED')) {
    $savedEnvironment[$envName] = [Environment]::GetEnvironmentVariable($envName, 'Process')
}
function Assert-NativeSuccess([string]$Operation) {
    if ($LASTEXITCODE -ne 0) { throw "$Operation failed (exit $LASTEXITCODE)" }
}
Push-Location $repoRoot
try {
    $revisionSHA = git rev-parse --verify "$Revision^{commit}"
    Assert-NativeSuccess 'Resolve release commit'
    $revisionDate = git show -s --format=%cI $revisionSHA
    Assert-NativeSuccess 'Read commit date'
    $outputDir = Join-Path $repoRoot ".local/releases/$Version"
    if (Test-Path -LiteralPath $outputDir) { throw "Output already exists: $outputDir. Use a fresh workspace for another build." }
    New-Item -ItemType Directory -Path $outputDir -Force | Out-Null
    $stageDir = Join-Path $outputDir 'stage'
    New-Item -ItemType Directory -Path $stageDir | Out-Null
    $sourceName = "s3webui_${Version}_source"
    $linuxName = "s3webui_${Version}_linux_amd64"
    $sourceTar = Join-Path $stageDir 'git-source.tar'
    git archive --format=tar "--prefix=$sourceName/" "--output=$sourceTar" $revisionSHA
    Assert-NativeSuccess 'Archive release source'
    tar -xf $sourceTar -C $stageDir
    Assert-NativeSuccess 'Extract release source'
    $sourceDir = Join-Path $stageDir $sourceName
    Push-Location $sourceDir
    try {
        $env:GOOS = 'linux'
        $env:GOARCH = 'amd64'
        $env:GOAMD64 = 'v1'
        $env:CGO_ENABLED = '0'
        go mod vendor
        Assert-NativeSuccess 'Vendor release dependencies'
        $goVersion = go version
        Assert-NativeSuccess 'Read Go version'
        $buildInfo = [ordered]@{ version=$Version; commit=$revisionSHA; buildDate=$revisionDate; toolchain=$goVersion; target='linux/amd64'; goamd64='v1'; cgoEnabled=$false }
        $buildInfo | ConvertTo-Json | Set-Content -LiteralPath 'BUILD-INFO.json' -Encoding utf8
        $linuxDir = Join-Path $stageDir $linuxName
        New-Item -ItemType Directory -Path $linuxDir | Out-Null
        $binary = Join-Path $linuxDir 's3webui'
        go build -mod=vendor -buildvcs=false -trimpath -ldflags "-s -w -X main.version=$Version -X main.commit=$revisionSHA -X main.buildDate=$revisionDate" -o $binary ./cmd/s3webui
        Assert-NativeSuccess 'Build Linux amd64 binary'
        # ELF64, little endian, EM_X86_64. Avoid publishing a Windows or ARM binary by mistake.
        $elf = [IO.File]::ReadAllBytes($binary)
        if ($elf.Length -lt 64 -or $elf[0] -ne 127 -or $elf[1] -ne 69 -or $elf[2] -ne 76 -or $elf[3] -ne 70 -or $elf[4] -ne 2 -or $elf[5] -ne 1 -or $elf[18] -ne 62 -or $elf[19] -ne 0) { throw 'Invalid Linux amd64 ELF binary' }
        go version -m $binary | Set-Content -LiteralPath (Join-Path $linuxDir 'GO-BUILD-INFO.txt') -Encoding utf8
        Assert-NativeSuccess 'Inspect Go binary'
        Copy-Item -LiteralPath LICENSE, NOTICE.md, THIRD_PARTY_LICENSES.md, .env.example, BUILD-INFO.json -Destination $linuxDir
        Copy-Item -LiteralPath 'docs/releases/README.release.md' -Destination $linuxDir
    } finally { Pop-Location }
    # go run must execute archive helpers on the build host, not the cross-compile target.
    $env:GOOS = go env GOHOSTOS
    Assert-NativeSuccess 'Read host OS'
    $env:GOARCH = go env GOHOSTARCH
    Assert-NativeSuccess 'Read host architecture'
    $sourceArchive = Join-Path $outputDir "$sourceName.tar.gz"
    go run (Join-Path $sourceDir 'scripts/archive-release.go') -directory $sourceDir -output $sourceArchive -epoch $revisionDate
    Assert-NativeSuccess 'Package corresponding source'
    Copy-Item -LiteralPath $sourceArchive -Destination (Join-Path $linuxDir 'source.tar.gz')
    $linuxArchive = Join-Path $outputDir "$linuxName.tar.gz"
    # Set deterministic Unix modes even when packaging on Windows.
    go run (Join-Path $sourceDir 'scripts/archive-release.go') -directory $linuxDir -output $linuxArchive -epoch $revisionDate -executable s3webui
    Assert-NativeSuccess 'Package Linux binary'
    $checksums = foreach ($asset in @($linuxArchive, $sourceArchive)) {
        $hash = (Get-FileHash -LiteralPath $asset -Algorithm SHA256).Hash.ToLowerInvariant()
        "$hash  $([IO.Path]::GetFileName($asset))"
    }
    [IO.File]::WriteAllText((Join-Path $outputDir 'SHA256SUMS'), ($checksums -join "`n") + "`n", [Text.UTF8Encoding]::new($false))
    Write-Output "Release assets: $outputDir"
    Write-Output "Source commit: $revisionSHA"
} finally {
    foreach ($envName in $savedEnvironment.Keys) { [Environment]::SetEnvironmentVariable($envName, $savedEnvironment[$envName], 'Process') }
    Pop-Location
}

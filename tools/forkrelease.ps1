#Requires -Version 7
<#
.SYNOPSIS
    Builds, signs and optionally publishes this fork's Windows release.

.DESCRIPTION
    Bumps the version in viva.toml and internal/update/update.go (the update
    checker reports this build's version), packages the NSIS installer, writes
    the signed latest.json the update feed reads, and — with -Publish — commits
    the bump, pushes it and creates the GitHub release with both assets.

    The versions must be plain dot-separated numbers: NSIS refuses a suffix,
    and update.Newer compares only numbers.

.EXAMPLE
    ./tools/forkrelease.ps1 0.3.1

.EXAMPLE
    ./tools/forkrelease.ps1 0.3.1 -NotesPath ./release-notes.md -Publish
#>
param(
    [Parameter(Mandatory = $true, Position = 0)][string]$Version,
    [string]$NotesPath = "",
    [string]$KeyPath = "",
    [string]$Branch = "feature/network-cli-exec",
    [string]$GoBinary = "",
    [switch]$Publish
)

$ErrorActionPreference = "Stop"
$repo = Split-Path $PSScriptRoot -Parent
$forkRepo = "NOTFROMCONCEN/motty-network-cli-exec"
$tag = "v$Version"

if ($Version -notmatch '^\d+\.\d+\.\d+$') {
    throw "version '$Version' must be three dot-separated numbers, for example 0.3.1"
}
if ($KeyPath -eq "") {
    $KeyPath = Join-Path (Split-Path $repo -Parent) ".tools/motty-fork-signing.key"
}
if (-not (Test-Path $KeyPath)) {
    throw "no signing key at $KeyPath; run 'go run ./tools/forkrelease keygen -private <path>' once"
}
if ($GoBinary -eq "") {
    # The build machine may have Go only beside the repository.
    $candidate = Join-Path (Split-Path $repo -Parent) ".tools/go/bin/go.exe"
    $GoBinary = if (Get-Command go -ErrorAction SilentlyContinue) { "go" } elseif (Test-Path $candidate) { $candidate } else { throw "go was not found on PATH" }
}

Push-Location $repo
try {
    # Untracked files (local notes, build output) do not go into a release, so
    # they must not block one either.
    $dirty = git status --porcelain --untracked-files=no
    if ($Publish -and $dirty) {
        throw "the working tree has uncommitted changes; commit or stash them before publishing"
    }

    # The app reports its version from update.Current, and a test keeps it equal
    # to viva.toml's, so both move together.
    $utf8 = [System.Text.UTF8Encoding]::new($false)
    $manifest = "viva.toml"
    $text = [IO.File]::ReadAllText((Join-Path $repo $manifest))
    $pattern = '(?m)^version\s*=\s*"[^"]+"'
    if (-not [regex]::IsMatch($text, $pattern)) { throw "no version line was found in viva.toml" }
    [IO.File]::WriteAllText((Join-Path $repo $manifest), [regex]::Replace($text, $pattern, "version = `"$Version`"", 1), $utf8)

    $current = "internal/update/update.go"
    $text = [IO.File]::ReadAllText((Join-Path $repo $current))
    $pattern = 'const Current = "[^"]+"'
    if (-not [regex]::IsMatch($text, $pattern)) { throw "no Current line was found in internal/update/update.go" }
    [IO.File]::WriteAllText((Join-Path $repo $current), [regex]::Replace($text, $pattern, "const Current = `"$Version`"", 1), $utf8)

    Write-Host "==> packaging Motty $Version" -ForegroundColor Cyan
    & $GoBinary run ../viva/cmd/viva package -C . --target windows --arch amd64 --format nsis
    if ($LASTEXITCODE -ne 0) { throw "viva package failed" }

    $setup = Join-Path $repo "build/dist/motty-$Version-windows-amd64-setup.exe"
    if (-not (Test-Path $setup)) { throw "the installer was not written to $setup" }

    Write-Host "==> signing the manifest" -ForegroundColor Cyan
    $out = Join-Path $repo "build/dist/latest.json"
    $signArgs = @("run", "./tools/forkrelease", "manifest", "-version", $Version, "-pkg", $setup, "-key", $KeyPath, "-out", $out, "-repo", $forkRepo)
    if ($NotesPath -ne "") { $signArgs += @("-notes", $NotesPath) }
    & $GoBinary @signArgs
    if ($LASTEXITCODE -ne 0) { throw "signing the manifest failed" }

    Write-Host ""
    Write-Host "built:" -ForegroundColor Green
    Write-Host "  $setup"
    Write-Host "  $out"

    if (-not $Publish) {
        Write-Host ""
        Write-Host "to publish:" -ForegroundColor Yellow
        Write-Host "  git add viva.toml internal/update/update.go"
        Write-Host "  git commit -m `"chore(release): $tag`""
        Write-Host "  git push fork $Branch"
        Write-Host "  gh release create $tag --repo $forkRepo --target $Branch --title `"Motty $tag`" --notes-file <notes> `"$setup`" `"$out`""
        return
    }

    Write-Host "==> committing the version bump" -ForegroundColor Cyan
    git add -- viva.toml internal/update/update.go
    git diff --cached --quiet
    if ($LASTEXITCODE -ne 0) {
        git commit -m "chore(release): $tag"
        if ($LASTEXITCODE -ne 0) { throw "git commit failed" }
    }
    else {
        Write-Host "    the version bump is already committed"
    }

    Write-Host "==> pushing to fork" -ForegroundColor Cyan
    git push fork $Branch
    if ($LASTEXITCODE -ne 0) { throw "git push failed" }

    Write-Host "==> creating the release" -ForegroundColor Cyan
    $releaseArgs = @("release", "create", $tag, "--repo", $forkRepo, "--target", $Branch, "--title", "Motty $tag")
    $releaseArgs += if ($NotesPath -ne "") { @("--notes-file", $NotesPath) } else { @("--generate-notes") }
    $releaseArgs += @($setup, $out)
    gh @releaseArgs
    if ($LASTEXITCODE -ne 0) { throw "gh release create failed" }

    Write-Host ""
    Write-Host "released $tag" -ForegroundColor Green
    Write-Host "installed builds check for it when 设置 → 通用 → 启动时检查更新 is on."
}
finally {
    Pop-Location
}

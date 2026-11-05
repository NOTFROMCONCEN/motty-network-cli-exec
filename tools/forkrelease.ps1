#Requires -Version 7
<#
.SYNOPSIS
    Builds, signs and optionally publishes this fork's Windows release.

.DESCRIPTION
    Bumps the version in viva.toml and internal/update/update.go (the update
    checker reports this build's version), packages the Windows installer and
    the Linux archive, writes the signed latest.json the update feed reads,
    and — with -Publish — commits the bump, pushes it and creates the GitHub
    release with all three assets.

    The versions must be plain dot-separated numbers: NSIS refuses a suffix,
    and update.Newer compares only numbers.

.EXAMPLE
    ./tools/forkrelease.ps1 0.3.1.4

.EXAMPLE
    ./tools/forkrelease.ps1 0.3.1.4 -NotesPath ./release-notes.md -Publish
#>
param(
    [Parameter(Mandatory = $true, Position = 0)][string]$Version,
    # The machine-facing version is the upstream base plus a build counter —
    # 0.3.1.4 sits on base 0.3.1 — and the published name carries the base and
    # the era's codename on top of it: 0.3.1.4 becomes
    # v0.3.1-netcli-build.4-arknights. The counter is the version's own last
    # number, so the two move together. Both stay until upstream publishes its
    # next version, when the base moves and a new codename may take over.
    [string]$Base = "0.3.2",
    [string]$Codename = "Arknights",
    [string]$NotesPath = "",
    [string]$KeyPath = "",
    [string]$Branch = "feature/network-cli-exec",
    [string]$GoBinary = "",
    [switch]$Publish
)

$ErrorActionPreference = "Stop"
$repo = Split-Path $PSScriptRoot -Parent
$forkRepo = "NOTFROMCONCEN/motty-network-cli-exec"
if ($Version -notmatch '^\d+\.\d+\.\d+\.\d+$') {
    throw "version '$Version' must be four dot-separated numbers — the upstream base plus the build counter — for example 0.3.1.4"
}
if ($Base -notmatch '^\d+\.\d+\.\d+$') {
    throw "base '$Base' must be three dot-separated numbers"
}
if (-not $Version.StartsWith("$Base.")) {
    throw "version '$Version' must sit on base '$Base': the first three numbers must agree"
}
$patch = [int]($Version -split '\.')[3]
if ($patch -lt 1) { throw "the build counter (the version's last number) must be at least 1" }
$tag = "v$Base-netcli-build.$patch-$($Codename.ToLowerInvariant())"
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

# Invoke-WithRetry runs a command and tries again while it fails: github.com
# refuses or resets connections often enough that a release should not die on
# one bad network moment.
function Invoke-WithRetry([string]$what, [scriptblock]$command) {
    for ($attempt = 1; $attempt -le 4; $attempt++) {
        if ($attempt -gt 1) {
            Write-Host "    $what failed; retrying (attempt $attempt of 4)" -ForegroundColor Yellow
            Start-Sleep -Seconds (20 * $attempt)
        }
        & $command
        if ($LASTEXITCODE -eq 0) { return }
    }
    throw "$what failed after 4 attempts"
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
    if ($LASTEXITCODE -ne 0) { throw "the windows package failed" }
    & $GoBinary run ../viva/cmd/viva package -C . --target linux --arch amd64 --format tar.gz
    if ($LASTEXITCODE -ne 0) { throw "the linux package failed" }

    # The names are the machine-facing ones internal/update.StableName builds:
    # the app picks its platform's file out of the manifest by these.
    $setup = Join-Path $repo "build/dist/motty-$Version-windows-amd64-setup.exe"
    if (-not (Test-Path $setup)) { throw "the installer was not written to $setup" }
    $tarball = Join-Path $repo "build/dist/motty-$Version-linux-amd64.tar.gz"
    if (-not (Test-Path $tarball)) { throw "the linux archive was not written to $tarball" }

    Write-Host "==> signing the manifest" -ForegroundColor Cyan
    $out = Join-Path $repo "build/dist/latest.json"
    $signArgs = @("run", "./tools/forkrelease", "manifest", "-version", $Version, "-base", $Base, "-codename", $Codename, "-pkg", $setup, "-pkg", $tarball, "-key", $KeyPath, "-out", $out, "-repo", $forkRepo)
    if ($NotesPath -ne "") { $signArgs += @("-notes", $NotesPath) }
    & $GoBinary @signArgs
    if ($LASTEXITCODE -ne 0) { throw "signing the manifest failed" }

    Write-Host ""
    Write-Host "built:" -ForegroundColor Green
    Write-Host "  $setup"
    Write-Host "  $tarball"
    Write-Host "  $out"

    if (-not $Publish) {
        Write-Host ""
        Write-Host "to publish:" -ForegroundColor Yellow
        Write-Host "  git add viva.toml internal/update/update.go"
        Write-Host "  git commit -m `"chore(release): $tag`""
        Write-Host "  git push fork $Branch"
        Write-Host "  gh release create $tag --repo $forkRepo --target $Branch --title `"Motty NetCLI $tag`" --notes-file <notes> `"$setup`" `"$tarball`" `"$out`""
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
    # The branch is rebased onto upstream regularly, so its history rewrites
    # and only a lease-checked force push reaches the fork.
    Invoke-WithRetry "git push fork $Branch" { git push --force-with-lease fork $Branch }

    Write-Host "==> creating the release" -ForegroundColor Cyan
    $releaseArgs = @("release", "create", $tag, "--repo", $forkRepo, "--target", $Branch, "--title", "Motty NetCLI $tag")
    $releaseArgs += if ($NotesPath -ne "") { @("--notes-file", $NotesPath) } else { @("--generate-notes") }
    $releaseArgs += @($setup, $tarball, $out)
    Invoke-WithRetry "gh release create $tag" { gh @releaseArgs }

    # The manifest is mirrored on the feed branch — one branch, one file — so
    # CDNs can serve it where github.com refuses connections. The branch is a
    # parentless commit holding only latest.json, replaced wholesale each
    # release.
    Write-Host "==> publishing the feed branch" -ForegroundColor Cyan
    $blob = git hash-object -w $out
    $tree = "100644 blob $blob`tlatest.json" | git mktree
    $feedCommit = git commit-tree $tree -m "feed: $tag"
    Invoke-WithRetry "git push -f fork feed" { git push -f fork "${feedCommit}:refs/heads/feed" }

    Write-Host ""
    Write-Host "released $tag" -ForegroundColor Green
    Write-Host "installed builds check for it when 设置 → 通用 → 启动时检查更新 is on."
}
finally {
    Pop-Location
}

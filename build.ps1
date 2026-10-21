# Builds MatchTUI for Windows.
#
#   .\build.ps1                 run checks and build dist\matchtui.exe
#   .\build.ps1 -SkipTests      skip the test run
param(
    [switch]$SkipTests
)

$ErrorActionPreference = "Stop"

$root = $PSScriptRoot
$dist = Join-Path $root "dist"
$exe  = Join-Path $dist "matchtui.exe"

Write-Host "matchtui: checking formatting and static analysis" -ForegroundColor Cyan
Push-Location $root
try {
    $unformatted = gofmt -l .
    if ($unformatted) {
        Write-Host "matchtui: these files are not gofmt clean:" -ForegroundColor Yellow
        $unformatted | ForEach-Object { Write-Host "  $_" }
        throw "run gofmt -w to fix"
    }

    go vet ./...
    if ($LASTEXITCODE -ne 0) { throw "go vet failed" }

    if (-not $SkipTests) {
        Write-Host "matchtui: running tests (use -SkipTests to skip)" -ForegroundColor Cyan
        go test ./...
        if ($LASTEXITCODE -ne 0) { throw "tests failed" }
    }

    New-Item -ItemType Directory -Force -Path $dist | Out-Null

    $env:CGO_ENABLED = "0"

    Write-Host "matchtui: building $exe" -ForegroundColor Cyan
    go build -trimpath -ldflags "-s -w" -o $exe .
    if ($LASTEXITCODE -ne 0) { throw "build failed" }
}
finally {
    Pop-Location
}

$size = [math]::Round((Get-Item $exe).Length / 1KB, 1)
Write-Host "matchtui: built $exe ($size KB)" -ForegroundColor Green

param(
    [string]$OutputDir = "dist",
    [string]$AppName = "config-center",
    [string]$DeployDir = "/www/wwwroot/config-service.baichengedu.com",
    [string]$ListenAddr = ":9313",
    [string]$ServiceUser = "www",
    [string]$BaseURL = "https://config-service.baichengedu.com"
)

$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $MyInvocation.MyCommand.Path
$dist = Join-Path $root $OutputDir
$packageDir = Join-Path $dist "$AppName-linux-amd64"
$binary = Join-Path $packageDir $AppName

function Write-Utf8NoBom {
    param(
        [string]$Path,
        [string[]]$Lines
    )
    $encoding = New-Object System.Text.UTF8Encoding($false)
    [System.IO.File]::WriteAllLines($Path, $Lines, $encoding)
}

New-Item -ItemType Directory -Force -Path $packageDir | Out-Null

Push-Location $root
try {
    go test ./...

    $env:CGO_ENABLED = "0"
    $env:GOOS = "linux"
    $env:GOARCH = "amd64"
    go build -trimpath -ldflags "-s -w -extldflags '-static'" -o $binary ./cmd/config-center

    Copy-Item -Path (Join-Path $root "deploy.env.example") -Destination (Join-Path $packageDir ".env.example") -Force
    Copy-Item -Path (Join-Path $root "README.md") -Destination (Join-Path $packageDir "README.md") -Force

    $serviceLines = @(
        "[Unit]",
        "Description=Config Center",
        "After=network.target",
        "",
        "[Service]",
        "Type=simple",
        "WorkingDirectory=$DeployDir",
        "ExecStart=$DeployDir/$AppName serve --env $DeployDir/.env --addr $ListenAddr --migrate",
        "Restart=always",
        "RestartSec=5",
        "User=$ServiceUser",
        "Environment=GIN_MODE=release",
        "",
        "[Install]",
        "WantedBy=multi-user.target"
    )
    Write-Utf8NoBom -Path (Join-Path $packageDir "$AppName.service") -Lines $serviceLines

    $btPanelLines = @(
        "Baota Panel Go project configuration",
        "",
        "Project path:",
        $DeployDir,
        "",
        "Executable:",
        "$DeployDir/$AppName",
        "",
        "Startup arguments:",
        "serve --env $DeployDir/.env --addr $ListenAddr --migrate",
        "",
        "Reverse proxy:",
        "$BaseURL -> http://127.0.0.1$ListenAddr",
        "",
        "Notes:",
        "Run init-db, migrate and register-service once over SSH. Let Baota Panel start, stop and restart the long-running service."
    )
    Write-Utf8NoBom -Path (Join-Path $packageDir "BT_PANEL_RUN.txt") -Lines $btPanelLines

    $shaHash = (Get-FileHash -Algorithm SHA256 -Path $binary).Hash
    $manifestLines = @(
        "app=$AppName",
        "target=linux/amd64",
        "cgo=0",
        "binary=$AppName",
        "sha256=$shaHash",
        "base_url=$BaseURL",
        "deploy_dir=$DeployDir",
        "listen_addr=$ListenAddr",
        "service_user=$ServiceUser"
    )
    Write-Utf8NoBom -Path (Join-Path $packageDir "BUILD_INFO.txt") -Lines $manifestLines

    Write-Host "Build complete: $packageDir"
    Write-Host "Binary: $binary"
    Write-Host "SHA256: $shaHash"
}
finally {
    Pop-Location
    Remove-Item Env:CGO_ENABLED -ErrorAction SilentlyContinue
    Remove-Item Env:GOOS -ErrorAction SilentlyContinue
    Remove-Item Env:GOARCH -ErrorAction SilentlyContinue
}

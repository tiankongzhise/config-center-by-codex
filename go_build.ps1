param(
    [string]$OutputDir = "dist",
    [string]$AppName = "config-center",
    [string]$DeployDir = "/www/wwwroot/config-service.baichengedu.com",
    [string]$ListenAddr = ":8080",
    [string]$ServiceUser = "www",
    [string]$BaseURL = "https://config-service.baichengedu.com"
)

$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $MyInvocation.MyCommand.Path
$dist = Join-Path $root $OutputDir
$packageDir = Join-Path $dist "$AppName-linux-amd64"
$binary = Join-Path $packageDir $AppName

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

    $serviceFile = @"
[Unit]
Description=Config Center
After=network.target

[Service]
Type=simple
WorkingDirectory=$DeployDir
ExecStart=$DeployDir/$AppName serve --env $DeployDir/.env --addr $ListenAddr --migrate
Restart=always
RestartSec=5
User=$ServiceUser
Environment=GIN_MODE=release

[Install]
WantedBy=multi-user.target
"@
    Set-Content -Path (Join-Path $packageDir "$AppName.service") -Value $serviceFile -Encoding UTF8

    $sha = Get-FileHash -Algorithm SHA256 -Path $binary
    $manifest = @"
app=$AppName
target=linux/amd64
cgo=0
binary=$AppName
sha256=$($sha.Hash)
base_url=$BaseURL
deploy_dir=$DeployDir
listen_addr=$ListenAddr
service_user=$ServiceUser
"@
    Set-Content -Path (Join-Path $packageDir "BUILD_INFO.txt") -Value $manifest -Encoding UTF8

    Write-Host "Build complete: $packageDir"
    Write-Host "Binary: $binary"
    Write-Host "SHA256: $($sha.Hash)"
}
finally {
    Pop-Location
    Remove-Item Env:CGO_ENABLED -ErrorAction SilentlyContinue
    Remove-Item Env:GOOS -ErrorAction SilentlyContinue
    Remove-Item Env:GOARCH -ErrorAction SilentlyContinue
}

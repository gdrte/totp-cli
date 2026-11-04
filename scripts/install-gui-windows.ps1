# Installs bin\totp-gui.exe for the current user (no admin rights needed):
# the app goes to %LOCALAPPDATA%\Programs\TOTP Authenticator, with a
# "TOTP Authenticator" entry in the Start menu. Run by `make install-gui`.
# -Uninstall removes both.
param([switch]$Uninstall)

$ErrorActionPreference = 'Stop'

$name = 'TOTP Authenticator'
$dir = Join-Path $env:LOCALAPPDATA "Programs\$name"
$exe = Join-Path $dir 'totp-gui.exe'
$shortcut = Join-Path ([Environment]::GetFolderPath('Programs')) "$name.lnk"

if ($Uninstall) {
    Remove-Item -Force -ErrorAction SilentlyContinue $shortcut
    Remove-Item -Recurse -Force -ErrorAction SilentlyContinue $dir
    Write-Host "Removed $name"
    exit 0
}

$source = Join-Path $PSScriptRoot '..\bin\totp-gui.exe'
if (-not (Test-Path $source)) {
    throw "bin\totp-gui.exe not found; run make gui first"
}

New-Item -ItemType Directory -Force $dir | Out-Null
try {
    Copy-Item -Force $source $exe
} catch {
    throw "Could not replace $exe. Close TOTP Authenticator and try again."
}

$link = (New-Object -ComObject WScript.Shell).CreateShortcut($shortcut)
$link.TargetPath = $exe
$link.WorkingDirectory = $dir
$link.Description = '2FA codes for you, your tests and your agents'
$link.Save()

Write-Host "Installed $exe"
Write-Host "Start menu: $name"

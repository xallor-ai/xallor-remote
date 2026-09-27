# Load MSVC + Windows SDK into the current PowerShell session for Tauri/cargo.
# Prefers vswhere / VS 2022 Build Tools; falls back to a known local install.
# Usage: . .\scripts\gui-msvc-env.ps1

$ErrorActionPreference = "Stop"

function Find-VsInstall {
  $vswhere = "${env:ProgramFiles(x86)}\Microsoft Visual Studio\Installer\vswhere.exe"
  if (Test-Path $vswhere) {
    $p = & $vswhere -latest -products * `
      -requires Microsoft.VisualStudio.Component.VC.Tools.x86.x64 `
      -property installationPath 2>$null
    if ($p -and (Test-Path (Join-Path $p "VC\Auxiliary\Build\vcvars64.bat"))) {
      return $p
    }
  }
  foreach ($cand in @(
    "${env:ProgramFiles(x86)}\Microsoft Visual Studio\2022\BuildTools",
    "D:\software\develop\visual_studio"
  )) {
    if (Test-Path (Join-Path $cand "VC\Auxiliary\Build\vcvars64.bat")) {
      return $cand
    }
  }
  return $null
}

$vs = Find-VsInstall
if (-not $vs) { throw "Cannot find Visual Studio / Build Tools with VC Tools." }

$vcvars = Join-Path $vs "VC\Auxiliary\Build\vcvars64.bat"
cmd /c "`"$vcvars`" >nul 2>&1 && set" | ForEach-Object {
  if ($_ -match '^([^=]+)=(.*)$') {
    [System.Environment]::SetEnvironmentVariable($matches[1], $matches[2], "Process")
  }
}
$env:Path = "$env:USERPROFILE\.cargo\bin;" + $env:Path

Write-Host ("VS=" + $vs)
Write-Host ("link=" + (Get-Command link.exe | Select-Object -ExpandProperty Source))
Write-Host ("rc=" + (Get-Command rc.exe | Select-Object -ExpandProperty Source))

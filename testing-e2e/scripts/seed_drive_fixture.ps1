param(
  [string]$Prefix = $(if ($env:VCLAW_E2E_PREFIX) { $env:VCLAW_E2E_PREFIX } else { "[VCLAW-E2E]" }),
  [string]$RunId = $("vclaw-e2e-seed-" + (Get-Date -Format "yyyyMMdd-HHmmss")),
  [string]$FolderName = "",
  [string]$OutDir = "testing-e2e/artifacts",
  [string]$VClawCommand = "go run ./cmd/vclaw",
  [string]$EnvFile = "testing-e2e/e2e.env.ps1"
)

$ErrorActionPreference = "Stop"
if (-not [string]::IsNullOrWhiteSpace($EnvFile) -and (Test-Path $EnvFile)) {
  . $EnvFile
  if ($env:VCLAW_E2E_PREFIX) { $Prefix = $env:VCLAW_E2E_PREFIX }
}

function Invoke-SeedCommand {
  param([string]$CommandLine, [string]$ArtifactDir, [string]$Name)
  $safeName = ($Name -replace '[^A-Za-z0-9_.-]', '_')
  $stdoutPath = Join-Path $ArtifactDir "$safeName.stdout.txt"
  $metaPath = Join-Path $ArtifactDir "$safeName.meta.json"
  $started = Get-Date
  $previousErrorActionPreference = $ErrorActionPreference
  $ErrorActionPreference = "Continue"
  try {
    $output = & cmd.exe /c $CommandLine 2>&1
    $exitCode = $LASTEXITCODE
  } finally {
    $ErrorActionPreference = $previousErrorActionPreference
  }
  $ended = Get-Date
  $text = ($output -join "`n")
  $text | Set-Content -Encoding utf8 -Path $stdoutPath
  [ordered]@{
    command = $CommandLine
    exit_code = $exitCode
    started_at = $started.ToUniversalTime().ToString("o")
    ended_at = $ended.ToUniversalTime().ToString("o")
    latency_ms = [int64]($ended - $started).TotalMilliseconds
    stdout_path = $stdoutPath
  } | ConvertTo-Json -Depth 10 | Set-Content -Encoding utf8 -Path $metaPath
  return [ordered]@{ exit_code = $exitCode; output = $text; stdout_path = $stdoutPath; meta_path = $metaPath }
}

function Get-JsonBlocksFromText {
  param([string]$Text)
  $blocks = @()
  $depth = 0
  $inString = $false
  $escaped = $false
  $start = -1
  for ($i = 0; $i -lt $Text.Length; $i++) {
    $ch = $Text[$i]
    if ($inString) {
      if ($escaped) { $escaped = $false }
      elseif ($ch -eq '\') { $escaped = $true }
      elseif ($ch -eq '"') { $inString = $false }
      continue
    }
    if ($ch -eq '"') { $inString = $true; continue }
    if ($ch -eq '{') { if ($depth -eq 0) { $start = $i }; $depth++; continue }
    if ($ch -eq '}') {
      if ($depth -gt 0) { $depth-- }
      if ($depth -eq 0 -and $start -ge 0) {
        $blocks += $Text.Substring($start, $i - $start + 1)
        $start = -1
      }
    }
  }
  return $blocks
}

function Extract-DriveFileFromOutput {
  param([string]$Output)
  foreach ($block in Get-JsonBlocksFromText -Text $Output) {
    try {
      $value = $block | ConvertFrom-Json
      if ($null -ne $value.Folder) { return $value.Folder }
      if ($null -ne $value.File) { return $value.File }
      if ($null -ne $value.ID -and $null -ne $value.Name) { return $value }
    } catch {
      continue
    }
  }
  return $null
}

function Set-E2EEnvFileValue {
  param([string]$Path, [string]$Name, [string]$Value)
  if ([string]::IsNullOrWhiteSpace($Path)) { return }
  $line = '$env:' + $Name + '="' + ($Value -replace '"', '`"') + '"'
  if (-not (Test-Path $Path)) {
    Set-Content -Encoding utf8 -Path $Path -Value $line
    return
  }
  $content = Get-Content -Path $Path
  $pattern = '^\$env:' + [regex]::Escape($Name) + '='
  $updated = $false
  $newContent = @()
  foreach ($existingLine in $content) {
    if ($existingLine -match $pattern) {
      $newContent += $line
      $updated = $true
    } else {
      $newContent += $existingLine
    }
  }
  if (-not $updated) { $newContent += $line }
  Set-Content -Encoding utf8 -Path $Path -Value $newContent
}

if ([string]::IsNullOrWhiteSpace($FolderName)) {
  $FolderName = "$Prefix fixtures run_id=$RunId"
}
if (-not $FolderName.Contains($Prefix)) {
  throw "FolderName phải chứa prefix $Prefix để cleanup/read-back an toàn."
}
if (-not $FolderName.Contains($RunId)) {
  $FolderName = "$FolderName run_id=$RunId"
}

$artifactDir = Join-Path $OutDir $RunId
New-Item -ItemType Directory -Force -Path $artifactDir | Out-Null

$createCommand = "$VClawCommand google drive create-folder -name `"$FolderName`""
$create = Invoke-SeedCommand -CommandLine $createCommand -ArtifactDir $artifactDir -Name "drive_create_folder"
$folder = Extract-DriveFileFromOutput -Output $create.output

$status = "pass"
$statusReason = "Drive fixture folder created and read back."
$readBack = $null
$folderID = ""
if ($create.exit_code -ne 0 -or $null -eq $folder) {
  $status = "fail"
  $statusReason = "Không tạo được Drive fixture folder hoặc không parse được output."
} else {
  $folderID = [string]$folder.ID
  if ([string]::IsNullOrWhiteSpace($folderID)) { $folderID = [string]$folder.Id }
  if ([string]::IsNullOrWhiteSpace($folderID)) {
    $status = "fail"
    $statusReason = "Drive create-folder không trả folder ID parse được."
  } else {
    $readBackCommand = "$VClawCommand google drive get -id $folderID"
    $readBack = Invoke-SeedCommand -CommandLine $readBackCommand -ArtifactDir $artifactDir -Name "drive_read_back_folder"
    if ($readBack.exit_code -ne 0 -or -not $readBack.output.Contains($RunId) -or -not $readBack.output.Contains($Prefix)) {
      $status = "fail"
      $statusReason = "Read-back Drive folder không thấy prefix/run_id."
    }
  }
}

$summary = [ordered]@{
  schema_version = "n2-e2e-seed-drive/v1"
  run_id = $RunId
  status = $status
  status_reason = $statusReason
  prefix = $Prefix
  folder_name = $FolderName
  folder_id = $folderID
  artifact_dir = $artifactDir
  commands = @(
    [ordered]@{ name = "drive_create_folder"; exit_code = $create.exit_code; stdout_path = $create.stdout_path },
    [ordered]@{ name = "drive_read_back_folder"; exit_code = if ($null -ne $readBack) { $readBack.exit_code } else { $null }; stdout_path = if ($null -ne $readBack) { $readBack.stdout_path } else { "" } }
  )
  suggested_env = [ordered]@{
    VCLAW_E2E_PREFIX = $Prefix
    VCLAW_E2E_DRIVE_FOLDER_ID = $folderID
  }
  cleanup_hint = if ([string]::IsNullOrWhiteSpace($folderID)) { "" } else { "$VClawCommand google drive trash -id $folderID" }
}

if ($status -eq "pass" -and -not [string]::IsNullOrWhiteSpace($folderID)) {
  Set-E2EEnvFileValue -Path $EnvFile -Name "VCLAW_E2E_PREFIX" -Value $Prefix
  Set-E2EEnvFileValue -Path $EnvFile -Name "VCLAW_E2E_DRIVE_FOLDER_ID" -Value $folderID
}

$summaryPath = Join-Path $artifactDir "seed-drive-summary.json"
$summary | ConvertTo-Json -Depth 30 | Set-Content -Encoding utf8 -Path $summaryPath

"seed_drive_summary: $summaryPath"
"status: $status"
if (-not [string]::IsNullOrWhiteSpace($folderID)) {
  ('$env:VCLAW_E2E_DRIVE_FOLDER_ID="{0}"' -f $folderID)
  "env_file_updated: $EnvFile"
  "cleanup_hint: $($summary.cleanup_hint)"
}
exit $(if ($status -eq "pass") { 0 } else { 1 })

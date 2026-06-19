param(
  [string]$Prefix = $(if ($env:VCLAW_E2E_PREFIX) { $env:VCLAW_E2E_PREFIX } else { "[VCLAW-E2E]" }),
  [string]$PeopleQuery = $(if ($env:VCLAW_E2E_PEOPLE_QUERY) { $env:VCLAW_E2E_PEOPLE_QUERY } else { "" }),
  [string]$OutDir = "testing-e2e/artifacts",
  [string]$VClawCommand = "go run ./cmd/vclaw",
  [int]$MaxResults = 10,
  [string]$EnvFile = "testing-e2e/e2e.env.ps1"
)

$ErrorActionPreference = "Stop"
if (-not [string]::IsNullOrWhiteSpace($EnvFile) -and (Test-Path $EnvFile)) {
  . $EnvFile
  if ($env:VCLAW_E2E_PREFIX) { $Prefix = $env:VCLAW_E2E_PREFIX }
  if ([string]::IsNullOrWhiteSpace($PeopleQuery) -and $env:VCLAW_E2E_PEOPLE_QUERY) { $PeopleQuery = $env:VCLAW_E2E_PEOPLE_QUERY }
}

function New-DiscoveryRunId {
  $stamp = Get-Date -Format "yyyyMMdd-HHmmss"
  return "vclaw-e2e-discover-$stamp"
}

function Invoke-DiscoveryCommand {
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
    name = $Name
    command = $CommandLine
    exit_code = $exitCode
    started_at = $started.ToUniversalTime().ToString("o")
    ended_at = $ended.ToUniversalTime().ToString("o")
    latency_ms = [int64]($ended - $started).TotalMilliseconds
    stdout_path = $stdoutPath
  } | ConvertTo-Json -Depth 10 | Set-Content -Encoding utf8 -Path $metaPath
  return [ordered]@{ name = $Name; command = $CommandLine; exit_code = $exitCode; output = $text; stdout_path = $stdoutPath; meta_path = $metaPath }
}

function Extract-EmailsFromText {
  param([string]$Text)
  $matches = [regex]::Matches($Text, '[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}')
  $items = New-Object System.Collections.Generic.List[string]
  foreach ($m in $matches) {
    $value = $m.Value.ToLowerInvariant()
    if (-not $items.Contains($value)) { $items.Add($value) }
  }
  return @($items)
}

function Extract-DriveIDsFromText {
  param([string]$Text)
  $items = @()
  foreach ($line in ($Text -split "`r?`n")) {
    if ($line -match '-\s+([^|]+)\|\s+([^|]+)\|\s+(.+)$') {
      $id = $matches[1].Trim()
      $name = $matches[2].Trim()
      $mime = $matches[3].Trim()
      if ($name.Contains($Prefix) -or $mime -match 'folder') {
        $items += [pscustomobject]@{ id = $id; name = $name; mime_type = $mime }
      }
    }
  }
  return $items
}

function Extract-ChatSpacesFromText {
  param([string]$Text)
  $items = @()
  foreach ($line in ($Text -split "`r?`n")) {
    if ($line -match '-\s+(spaces/[^|\s]+)\s+\|\s+([^|]+)\|\s+(.+)$') {
      $space = $matches[1].Trim()
      $display = $matches[2].Trim()
      $type = $matches[3].Trim()
      $items += [pscustomobject]@{ space = $space; display_name = $display; type = $type }
    }
  }
  return $items
}

function Set-E2EEnvFileValue {
  param([string]$Path, [string]$Name, [string]$Value)
  if ([string]::IsNullOrWhiteSpace($Path) -or [string]::IsNullOrWhiteSpace($Value)) { return }
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

$runId = New-DiscoveryRunId
$artifactDir = Join-Path $OutDir $runId
New-Item -ItemType Directory -Force -Path $artifactDir | Out-Null

$commands = New-Object System.Collections.Generic.List[object]

$commands.Add((Invoke-DiscoveryCommand -ArtifactDir $artifactDir -Name "gmail_profile" -CommandLine "$VClawCommand google gmail profile"))
$commands.Add((Invoke-DiscoveryCommand -ArtifactDir $artifactDir -Name "gmail_prefix_search" -CommandLine "$VClawCommand google gmail list -query `"subject:$Prefix OR `"$Prefix`"`" -max-results $MaxResults"))
$commands.Add((Invoke-DiscoveryCommand -ArtifactDir $artifactDir -Name "drive_prefix_search" -CommandLine "$VClawCommand google drive list -query `"name contains '$Prefix' and trashed = false`" -max-results $MaxResults"))
$commands.Add((Invoke-DiscoveryCommand -ArtifactDir $artifactDir -Name "chat_spaces" -CommandLine "$VClawCommand google chat list-spaces -page-size $MaxResults"))
if (-not [string]::IsNullOrWhiteSpace($PeopleQuery)) {
  $commands.Add((Invoke-DiscoveryCommand -ArtifactDir $artifactDir -Name "people_search" -CommandLine "$VClawCommand google people search-directory -query `"$PeopleQuery`" -max-results $MaxResults"))
}

$allText = ($commands | ForEach-Object { $_.output }) -join "`n"
$emails = @(Extract-EmailsFromText -Text $allText)
$drive = @(Extract-DriveIDsFromText -Text (($commands | Where-Object { $_.name -eq "drive_prefix_search" }).output))
$spaces = @(Extract-ChatSpacesFromText -Text (($commands | Where-Object { $_.name -eq "chat_spaces" }).output))

$targetEmail = if ($env:VCLAW_E2E_TARGET_EMAIL) { $env:VCLAW_E2E_TARGET_EMAIL } elseif ($emails.Count -gt 0) { [string]$emails[0] } else { "" }
$driveFolder = if ($env:VCLAW_E2E_DRIVE_FOLDER_ID) { $env:VCLAW_E2E_DRIVE_FOLDER_ID } else { "" }
foreach ($item in $drive) {
  if ([string]::IsNullOrWhiteSpace($driveFolder) -and ([string]$item.mime_type) -match 'folder') {
    $driveFolder = [string]$item.id
  }
}
$chatSpace = if ($env:VCLAW_E2E_CHAT_SPACE) { $env:VCLAW_E2E_CHAT_SPACE } elseif ($spaces.Count -gt 0) { [string]$spaces[0].space } else { "" }

$summary = [ordered]@{
  schema_version = "n2-e2e-discovery/v1"
  run_id = $runId
  status = "pass"
  prefix = $Prefix
  artifact_dir = $artifactDir
  commands = @($commands | ForEach-Object { [ordered]@{ name = $_.name; exit_code = $_.exit_code; stdout_path = $_.stdout_path } })
  candidates = [ordered]@{
    target_emails = @($emails)
    drive_items = @($drive)
    chat_spaces = @($spaces)
    calendar_ids = @("primary")
  }
  suggested_env = [ordered]@{
    VCLAW_E2E_PREFIX = $Prefix
    VCLAW_E2E_TARGET_EMAIL = $targetEmail
    VCLAW_E2E_CALENDAR_ID = "primary"
    VCLAW_E2E_DRIVE_FOLDER_ID = $driveFolder
    VCLAW_E2E_CHAT_SPACE = $chatSpace
  }
  notes = @(
    "Discovery chỉ đọc Google API qua CLI hiện có.",
    "Calendar list ID chưa có CLI riêng; dùng primary làm candidate mặc định cho test account.",
    "Chỉ export các env mà bạn đã xác nhận là fixture test an toàn."
  )
}

Set-E2EEnvFileValue -Path $EnvFile -Name "VCLAW_E2E_PREFIX" -Value $summary.suggested_env.VCLAW_E2E_PREFIX
Set-E2EEnvFileValue -Path $EnvFile -Name "VCLAW_E2E_TARGET_EMAIL" -Value $summary.suggested_env.VCLAW_E2E_TARGET_EMAIL
Set-E2EEnvFileValue -Path $EnvFile -Name "VCLAW_E2E_CALENDAR_ID" -Value $summary.suggested_env.VCLAW_E2E_CALENDAR_ID
Set-E2EEnvFileValue -Path $EnvFile -Name "VCLAW_E2E_DRIVE_FOLDER_ID" -Value $summary.suggested_env.VCLAW_E2E_DRIVE_FOLDER_ID
Set-E2EEnvFileValue -Path $EnvFile -Name "VCLAW_E2E_CHAT_SPACE" -Value $summary.suggested_env.VCLAW_E2E_CHAT_SPACE

$summaryPath = Join-Path $artifactDir "discovery-summary.json"
$summary | ConvertTo-Json -Depth 30 | Set-Content -Encoding utf8 -Path $summaryPath

"discovery_summary: $summaryPath"
"env_file_updated: $EnvFile"
"Suggested env:"
('$env:VCLAW_E2E_PREFIX="{0}"' -f $summary.suggested_env.VCLAW_E2E_PREFIX)
if (-not [string]::IsNullOrWhiteSpace($summary.suggested_env.VCLAW_E2E_TARGET_EMAIL)) { ('$env:VCLAW_E2E_TARGET_EMAIL="{0}"' -f $summary.suggested_env.VCLAW_E2E_TARGET_EMAIL) }
('$env:VCLAW_E2E_CALENDAR_ID="{0}"' -f $summary.suggested_env.VCLAW_E2E_CALENDAR_ID)
if (-not [string]::IsNullOrWhiteSpace($summary.suggested_env.VCLAW_E2E_DRIVE_FOLDER_ID)) { ('$env:VCLAW_E2E_DRIVE_FOLDER_ID="{0}"' -f $summary.suggested_env.VCLAW_E2E_DRIVE_FOLDER_ID) }
if (-not [string]::IsNullOrWhiteSpace($summary.suggested_env.VCLAW_E2E_CHAT_SPACE)) { ('$env:VCLAW_E2E_CHAT_SPACE="{0}"' -f $summary.suggested_env.VCLAW_E2E_CHAT_SPACE) }

param(
    [switch]$RunTests,
    [switch]$SkipGitNexus
)

$ErrorActionPreference = "Stop"
$repoRoot = Resolve-Path (Join-Path $PSScriptRoot "..\..")
Set-Location $repoRoot

$failures = New-Object System.Collections.Generic.List[string]
$warnings = New-Object System.Collections.Generic.List[string]

function Add-Failure([string]$message) { [void]$failures.Add($message) }
function Add-Warning([string]$message) { [void]$warnings.Add($message) }
function Get-DotEnvValue([string]$key) {
    $envPath = Join-Path $repoRoot ".env"
    if (!(Test-Path $envPath)) { return $null }
    $line = Get-Content $envPath | Where-Object { $_ -match "^\s*$([regex]::Escape($key))\s*=" } | Select-Object -Last 1
    if ($null -eq $line) { return $null }
    return (($line -split "=", 2)[1]).Trim()
}

Write-Host "V-Claw release check" -ForegroundColor Cyan
Write-Host "Repo: $repoRoot"

if (!(Test-Path ".env")) {
    Add-Warning ".env not found; runtime may rely on shell environment. Copy .env.example for release."
}

$skillNudge = Get-DotEnvValue "VCLAW_SKILL_NUDGE_INTERVAL"
if ($null -eq $skillNudge) {
    Add-Warning "VCLAW_SKILL_NUDGE_INTERVAL is not set in .env; runtime default is expected to be 0."
} elseif ($skillNudge -ne "0") {
    Add-Failure "VCLAW_SKILL_NUDGE_INTERVAL must be 0 for this release unless skill review/audit gates are implemented. Current: $skillNudge"
}

$requiredFiles = @(
    "docs/production-harness-review.md",
    "docs/04-sequences.md",
    "README.md",
    "configs/google/README.md",
    "internal/channels/README.md"
)
foreach ($path in $requiredFiles) {
    if (!(Test-Path $path)) { Add-Failure "Missing release/reference file: $path" }
}

$openAIKey = Get-DotEnvValue "OPENAI_API_KEY"
if ([string]::IsNullOrWhiteSpace($openAIKey)) { Add-Warning "OPENAI_API_KEY is empty in .env; provider startup will fail unless set in shell." }

$telegramToken = Get-DotEnvValue "TELEGRAM_BOT_TOKEN"
if ([string]::IsNullOrWhiteSpace($telegramToken)) { Add-Warning "TELEGRAM_BOT_TOKEN is empty; Telegram release smoke cannot run." }

$googleMode = Get-DotEnvValue "VCLAW_GOOGLE_TOOLS_MODE"
if ($googleMode -eq "required") {
    $cred = Get-DotEnvValue "VCLAW_GOOGLE_CREDENTIALS_PATH"
    $token = Get-DotEnvValue "VCLAW_GOOGLE_TOKEN_PATH"
    if ([string]::IsNullOrWhiteSpace($cred) -or !(Test-Path $cred)) { Add-Failure "Google tools required but credentials file is missing: $cred" }
    if ([string]::IsNullOrWhiteSpace($token) -or !(Test-Path $token)) { Add-Failure "Google tools required but token file is missing: $token" }
}

if ($RunTests) {
    Write-Host "Running go test ./..." -ForegroundColor Cyan
    go test ./...
    if ($LASTEXITCODE -ne 0) { Add-Failure "go test ./... failed" }
}

if (!$SkipGitNexus) {
    Write-Host "Running GitNexus detect-changes" -ForegroundColor Cyan
    $gitNexusOutput = npx gitnexus detect-changes --repo V-Claw 2>&1
    $gitNexusText = ($gitNexusOutput | Out-String).Trim()
    if ($gitNexusText) { Write-Host $gitNexusText }
    if ($LASTEXITCODE -ne 0 -or $gitNexusText -match "(?i)\b(error|failed|eperm)\b") {
        Add-Warning "GitNexus detect-changes did not complete cleanly; run manually before release."
    }
}

if ($warnings.Count -gt 0) {
    Write-Host "`nWarnings:" -ForegroundColor Yellow
    foreach ($warning in $warnings) { Write-Host "- $warning" -ForegroundColor Yellow }
}

if ($failures.Count -gt 0) {
    Write-Host "`nRelease check failed:" -ForegroundColor Red
    foreach ($failure in $failures) { Write-Host "- $failure" -ForegroundColor Red }
    exit 1
}

Write-Host "`nRelease check passed." -ForegroundColor Green

param(
  [Parameter(Mandatory=$true)]
  [string]$RunId,
  [string]$Prefix = $(if ($env:VCLAW_E2E_PREFIX) { $env:VCLAW_E2E_PREFIX } else { "[VCLAW-E2E]" })
)

$ErrorActionPreference = "Stop"

@"
Cleanup skeleton chưa xóa object thật.

Chỉ được cleanup object thỏa cả hai điều kiện:
- chứa prefix: $Prefix
- chứa run_id: $RunId

Khi implement cleanup thật, không được xóa object không có run_id trùng scenario.
"@

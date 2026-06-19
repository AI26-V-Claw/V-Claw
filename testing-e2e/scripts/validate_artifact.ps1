param(
  [Parameter(Mandatory=$true)]
  [string]$SummaryPath
)

$ErrorActionPreference = "Stop"

if (-not (Test-Path $SummaryPath)) {
  throw "Không tìm thấy summary artifact: $SummaryPath"
}

$summary = Get-Content -Raw -Path $SummaryPath | ConvertFrom-Json
$errors = New-Object System.Collections.Generic.List[string]
$validStatuses = @("pass", "fail", "blocked_env", "pending_verification")

function Add-ErrorIfMissing {
  param([object]$Object, [string]$Name)
  if ($null -eq $Object.$Name -or [string]::IsNullOrWhiteSpace([string]$Object.$Name)) {
    $script:errors.Add("missing_required_field:$Name")
  }
}

Add-ErrorIfMissing -Object $summary -Name "schema_version"
Add-ErrorIfMissing -Object $summary -Name "scenario_id"
Add-ErrorIfMissing -Object $summary -Name "run_id"
Add-ErrorIfMissing -Object $summary -Name "status"
Add-ErrorIfMissing -Object $summary -Name "status_reason"

if ($validStatuses -notcontains $summary.status) {
  $errors.Add("invalid_status:$($summary.status)")
}

if ($summary.status -eq "pass") {
  if ($summary.readiness_counted -ne $true) {
    $errors.Add("pass_requires_readiness_counted_true")
  }
} else {
  if ($summary.readiness_counted -ne $false) {
    $errors.Add("non_pass_requires_readiness_counted_false")
  }
}

if ($summary.status -eq "blocked_env") {
  if ($null -eq $summary.missing_env -or $summary.missing_env.Count -eq 0) {
    $errors.Add("blocked_env_requires_missing_env")
  }
  if ([string]::IsNullOrWhiteSpace([string]$summary.verify_again)) {
    $errors.Add("blocked_env_requires_verify_again")
  }
}

if ($null -eq $summary.trace) {
  $errors.Add("missing_trace")
} elseif ($null -eq $summary.trace.run_ids -or $summary.trace.run_ids.Count -eq 0) {
  $errors.Add("trace_requires_run_ids")
}

if ($null -ne $summary.objects_written) {
  foreach ($object in $summary.objects_written) {
    $hasRunIdFlag = $false
    if ($null -ne $object.run_id_present) {
      $hasRunIdFlag = [bool]$object.run_id_present
    }
    $hasRunIdText = $false
    if ($null -ne $object.run_id) {
      $hasRunIdText = -not [string]::IsNullOrWhiteSpace([string]$object.run_id)
    }
    if (-not ($hasRunIdFlag -or $hasRunIdText)) {
      $errors.Add("written_object_missing_run_id:$($object.object_id)")
    }
  }
}

$result = [ordered]@{
  summary_path = $SummaryPath
  valid = ($errors.Count -eq 0)
  errors = @($errors)
}

$result | ConvertTo-Json -Depth 10
if ($errors.Count -gt 0) {
  exit 1
}

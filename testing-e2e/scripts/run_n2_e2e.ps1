param(
  [Parameter(Mandatory=$true)]
  [string]$Scenario,
  [switch]$DryRun,
  [switch]$RunChat,
  [string]$OutDir = "testing-e2e/artifacts",
  [string]$VClawCommand = "go run ./cmd/vclaw",
  [string]$EnvFile = "testing-e2e/e2e.env.ps1"
)

$ErrorActionPreference = "Stop"
if (-not ([System.Management.Automation.PSTypeName]'VClawE2EInteractiveProcess').Type) {
  Add-Type -TypeDefinition @"
using System;
using System.Collections.Concurrent;
using System.Diagnostics;
using System.IO;
using System.Threading.Tasks;

public class VClawE2EInteractiveProcess {
  private Process process;
  private readonly ConcurrentQueue<string> lines = new ConcurrentQueue<string>();

  public void Start(string command, string workingDirectory) {
    var startInfo = new ProcessStartInfo();
    startInfo.FileName = "cmd.exe";
    startInfo.Arguments = "/c " + command;
    startInfo.WorkingDirectory = workingDirectory;
    startInfo.UseShellExecute = false;
    startInfo.RedirectStandardInput = true;
    startInfo.RedirectStandardOutput = true;
    startInfo.RedirectStandardError = true;
    startInfo.CreateNoWindow = true;
    process = new Process();
    process.StartInfo = startInfo;
    process.Start();
    Task.Run(() => ReadLines(process.StandardOutput));
    Task.Run(() => ReadLines(process.StandardError));
  }

  private void ReadLines(StreamReader reader) {
    string line;
    while ((line = reader.ReadLine()) != null) {
      lines.Enqueue(line);
    }
  }

  public void WriteLine(string line) {
    process.StandardInput.WriteLine(line);
    process.StandardInput.Flush();
  }

  public void CloseInput() { process.StandardInput.Close(); }
  public bool HasExited { get { return process.HasExited; } }
  public int ExitCode { get { return process.ExitCode; } }
  public bool WaitForExit(int milliseconds) { return process.WaitForExit(milliseconds); }
  public void Kill() { if (!process.HasExited) process.Kill(); }
  public string[] Lines() { return lines.ToArray(); }
}
"@
}
$RepoRoot = Resolve-Path (Join-Path $PSScriptRoot "..\..")
if (-not [System.IO.Path]::IsPathRooted($OutDir)) { $OutDir = Join-Path $RepoRoot $OutDir }
if (-not [string]::IsNullOrWhiteSpace($EnvFile) -and -not [System.IO.Path]::IsPathRooted($EnvFile)) { $EnvFile = Join-Path $RepoRoot $EnvFile }
if (-not [string]::IsNullOrWhiteSpace($EnvFile) -and (Test-Path $EnvFile)) {
  . $EnvFile
}

function New-RunId {
  $stamp = Get-Date -Format "yyyyMMdd-HHmmss"
  return "vclaw-e2e-$stamp"
}

function Get-EnvPresence {
  param([string[]]$Names)
  $result = @{}
  foreach ($name in $Names) {
    if ([string]::IsNullOrWhiteSpace($name)) { continue }
    $value = [Environment]::GetEnvironmentVariable($name)
    $result[$name] = -not [string]::IsNullOrWhiteSpace($value)
  }
  return $result
}

function Get-FixturesSnapshot {
  return [ordered]@{
    prefix = if ($env:VCLAW_E2E_PREFIX) { $env:VCLAW_E2E_PREFIX } else { "[VCLAW-E2E]" }
    target_email = $env:VCLAW_E2E_TARGET_EMAIL
    secondary_email = $env:VCLAW_E2E_SECONDARY_EMAIL
    calendar_id = $env:VCLAW_E2E_CALENDAR_ID
    drive_folder_id = $env:VCLAW_E2E_DRIVE_FOLDER_ID
    chat_space = $env:VCLAW_E2E_CHAT_SPACE
    chat_member_email = $env:VCLAW_E2E_CHAT_MEMBER_EMAIL
  }
}

function New-BaseSummary {
  param(
    [object]$ScenarioDoc,
    [string]$RunId,
    [string]$Status,
    [string]$Reason,
    [bool]$ReadinessCounted,
    [string]$ArtifactDir,
    [string[]]$MissingEnv = @()
  )
  return [ordered]@{
    schema_version = "n2-e2e/v1"
    scenario_id = $ScenarioDoc.scenario_id
    run_id = $RunId
    status = $Status
    status_reason = $Reason
    readiness_counted = $ReadinessCounted
    started_at = (Get-Date).ToUniversalTime().ToString("o")
    ended_at = (Get-Date).ToUniversalTime().ToString("o")
    latency_ms = 0
    env = Get-EnvPresence -Names ($ScenarioDoc.fixtures.required_env + $ScenarioDoc.fixtures.optional_env)
    fixtures = Get-FixturesSnapshot
    expected_tool_calls = @()
    actual_tool_calls = @()
    approvals = @()
    objects_written = @()
    read_back_assertions = @()
    idempotency_assertions = @()
    trace = [ordered]@{
      session_id = "e2e-$RunId"
      run_ids = @($RunId)
      action_ids = @()
      approval_ids = @()
      tool_call_ids = @()
    }
    hard_assertions = @()
    llm_judge = [ordered]@{
      enabled = $false
      score = $null
      notes = ""
    }
    cleanup = [ordered]@{
      attempted = $false
      status = "pending_verification"
      objects_deleted_or_reverted = @()
    }
    missing_env = $MissingEnv
    verify_again = "./testing-e2e/scripts/run_n2_e2e.ps1 -Scenario $($ScenarioDoc.scenario_id) -RunChat"
    artifact_dir = $ArtifactDir
  }
}

function Save-Summary {
  param([object]$Summary, [string]$ArtifactDir)
  $summaryPath = Join-Path $ArtifactDir "summary.json"
  $Summary.ended_at = (Get-Date).ToUniversalTime().ToString("o")
  $Summary | ConvertTo-Json -Depth 30 | Set-Content -Encoding utf8 -Path $summaryPath
  return $summaryPath
}

function Resolve-PromptTemplate {
  param([string]$Template, [string]$RunId)
  $prefix = if ($env:VCLAW_E2E_PREFIX) { $env:VCLAW_E2E_PREFIX } else { "[VCLAW-E2E]" }
  $chatSpace = if ($env:VCLAW_E2E_CHAT_SPACE) { $env:VCLAW_E2E_CHAT_SPACE } else { "" }
  $driveFolder = if ($env:VCLAW_E2E_DRIVE_FOLDER_ID) { $env:VCLAW_E2E_DRIVE_FOLDER_ID } else { "" }
  $targetEmail = if ($env:VCLAW_E2E_TARGET_EMAIL) { $env:VCLAW_E2E_TARGET_EMAIL } else { "" }
  return $Template.Replace("{{prefix}}", $prefix).Replace("{{run_id}}", $RunId).Replace("{{chat_space}}", $chatSpace).Replace("{{drive_folder_id}}", $driveFolder).Replace("{{target_email}}", $targetEmail)
}

function Get-ScriptedInputLines {
  param([object]$ScenarioDoc, [string]$RunId)
  $prompt = Resolve-PromptTemplate -Template ([string]$ScenarioDoc.prompt_template) -RunId $RunId
  $lines = [System.Collections.Generic.List[string]]::new()
  [void]$lines.Add($prompt)

  foreach ($decision in @($ScenarioDoc.scripted_decisions)) {
    switch ([string]$decision.decision) {
      "approve" { [void]$lines.Add("approve") }
      "reject" { [void]$lines.Add("reject") }
      "revise" {
        $comment = "revise nội dung để giữ prefix và run_id=$RunId"
        if ($null -ne $decision.revision -and $null -ne $decision.revision.append_text) {
          $comment = "revise $($decision.revision.append_text) run_id=$RunId"
        }
        [void]$lines.Add($comment)
      }
      "cancel" { [void]$lines.Add("cancel") }
      "duplicate" { [void]$lines.Add("approve") }
      default { }
    }
  }

  foreach ($followUp in @($ScenarioDoc.scripted_followups)) {
    $text = [string]$followUp.message
    if (-not [string]::IsNullOrWhiteSpace($text)) {
      $resolved = Resolve-PromptTemplate -Template $text -RunId $RunId
      [void]$lines.Add($resolved)
    }
  }

  [void]$lines.Add("/exit")
  return ,([string[]]$lines.ToArray())
}

function Get-ScriptedUserMessages {
  param([object]$ScenarioDoc, [string]$RunId)
  $messages = [System.Collections.Generic.List[string]]::new()
  $prompt = Resolve-PromptTemplate -Template ([string]$ScenarioDoc.prompt_template) -RunId $RunId
  [void]$messages.Add($prompt)

  foreach ($followUp in @($ScenarioDoc.scripted_followups)) {
    $text = [string]$followUp.message
    if (-not [string]::IsNullOrWhiteSpace($text)) {
      $resolved = Resolve-PromptTemplate -Template $text -RunId $RunId
      [void]$messages.Add($resolved)
    }
  }

  return ,([string[]]$messages.ToArray())
}

function Get-ScriptedApprovalDecisions {
  param([object]$ScenarioDoc, [string]$RunId)
  $decisions = [System.Collections.Generic.List[string]]::new()
  foreach ($decision in @($ScenarioDoc.scripted_decisions)) {
    switch ([string]$decision.decision) {
      "approve" { [void]$decisions.Add("approve") }
      "reject" { [void]$decisions.Add("reject") }
      "revise" {
        $comment = "revise nội dung để giữ prefix và run_id=$RunId"
        if ($null -ne $decision.revision -and $null -ne $decision.revision.append_text) {
          $comment = "revise $($decision.revision.append_text) run_id=$RunId"
        }
        [void]$decisions.Add($comment)
      }
      "cancel" { [void]$decisions.Add("cancel") }
      "duplicate" { [void]$decisions.Add("approve") }
      default { }
    }
  }
  return ,([string[]]$decisions.ToArray())
}

function Get-LastVClawChatResponse {
  param([string]$RawOutput)
  $last = $null
  foreach ($block in Get-JsonBlocksFromText -Text $RawOutput) {
    try {
      $value = $block | ConvertFrom-Json
      if ($null -ne $value.status -or $null -ne $value.requestId -or $null -ne $value.approvalRequest -or $null -ne $value.toolResults) {
        $last = $value
      }
    } catch {
      continue
    }
  }
  return $last
}


function Wait-VClawChatResponse {
  param(
    [object]$OutputLines,
    [int]$PreviousCount,
    [object]$Process,
    [int]$TimeoutSeconds = 300
  )
  $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
  while ((Get-Date) -lt $deadline) {
    $raw = ([string[]]$OutputLines.Lines()) -join "`n"
    $responses = New-Object System.Collections.ArrayList
    foreach ($block in Get-JsonBlocksFromText -Text $raw) {
      try {
        $value = $block | ConvertFrom-Json
        if ($null -ne $value.status -or $null -ne $value.requestId -or $null -ne $value.approvalRequest -or $null -ne $value.toolResults) {
          [void]$responses.Add($value)
        }
      } catch {
        continue
      }
    }
    if ($responses.Count -gt $PreviousCount) {
      return [pscustomobject]@{ count = $responses.Count; response = $responses[$responses.Count - 1] }
    }
    if ($Process.HasExited) { break }
    Start-Sleep -Milliseconds 500
  }
  return [pscustomobject]@{ count = $PreviousCount; response = $null }
}
function Invoke-VClawChatScript {
  param(
    [object]$ScenarioDoc,
    [string]$RunId,
    [string]$ArtifactDir,
    [string]$CommandLine
  )

  $sessionId = "e2e-$RunId"
  $maxIterations = 20
  if ($null -ne $ScenarioDoc.agent -and $null -ne $ScenarioDoc.agent.max_iterations) {
    $maxIterations = [int]$ScenarioDoc.agent.max_iterations
  }
  $inputLines = Get-ScriptedInputLines -ScenarioDoc $ScenarioDoc -RunId $RunId
  $userMessages = Get-ScriptedUserMessages -ScenarioDoc $ScenarioDoc -RunId $RunId
  $approvalDecisions = Get-ScriptedApprovalDecisions -ScenarioDoc $ScenarioDoc -RunId $RunId
  $inputPath = Join-Path $ArtifactDir "scripted-input.txt"
  $stdoutPath = Join-Path $ArtifactDir "vclaw-chat-stdout.txt"
  $stderrPath = Join-Path $ArtifactDir "vclaw-chat-stderr.txt"
  $exitPath = Join-Path $ArtifactDir "vclaw-chat-exit.json"
  $inputLines | Set-Content -Encoding utf8 -Path $inputPath

  $langfusePublic = [Environment]::GetEnvironmentVariable("LANGFUSE_PUBLIC_KEY")
  $langfuseSecret = [Environment]::GetEnvironmentVariable("LANGFUSE_SECRET_KEY")
  if ([string]::IsNullOrWhiteSpace($langfusePublic) -or [string]::IsNullOrWhiteSpace($langfuseSecret)) {
    [Environment]::SetEnvironmentVariable("LANGFUSE_PUBLIC_KEY", $null, "Process")
    [Environment]::SetEnvironmentVariable("LANGFUSE_SECRET_KEY", $null, "Process")
  }
  $command = "$CommandLine agent chat -session $sessionId -channel e2e-terminal -json -trace -max-iterations $maxIterations"
  $started = Get-Date
  $previousErrorActionPreference = $ErrorActionPreference
  $ErrorActionPreference = "Continue"
  $allOutput = $null
  $exitCode = 0
  $process = $null
  try {
    $process = [VClawE2EInteractiveProcess]::new()
    $process.Start($command, (Get-Location).Path)
    $allOutput = $process
    $turn = 0
    $approvalIndex = 0
    $responseCount = 0

    foreach ($message in $userMessages) {
      $turn++
      $turnInputPath = Join-Path $ArtifactDir ("interactive-input-{0:00}.txt" -f $turn)
      @($message) | Set-Content -Encoding utf8 -Path $turnInputPath
      $process.WriteLine($message)

      $waitResult = Wait-VClawChatResponse -OutputLines $allOutput -PreviousCount $responseCount -Process $process
      $responseCount = $waitResult.count
      $lastResponse = $waitResult.response
      while ($null -ne $lastResponse -and [string]$lastResponse.status -eq "approval_required") {
        if ($approvalIndex -lt $approvalDecisions.Count) {
          $decision = $approvalDecisions[$approvalIndex]
          $approvalIndex++
        } else {
          $decision = "approve"
        }
        $turn++
        $approvalInputPath = Join-Path $ArtifactDir ("interactive-input-{0:00}.txt" -f $turn)
        @($decision) | Set-Content -Encoding utf8 -Path $approvalInputPath
        $process.WriteLine($decision)
        $waitResult = Wait-VClawChatResponse -OutputLines $allOutput -PreviousCount $responseCount -Process $process
        $responseCount = $waitResult.count
        $lastResponse = $waitResult.response
      }
    }

    $process.WriteLine("/exit")
    $process.CloseInput()
    [void]$process.WaitForExit(600000)
    $exitCode = $process.ExitCode
  } finally {
    if ($null -ne $process -and -not $process.HasExited) { $process.Kill() }
    $ErrorActionPreference = $previousErrorActionPreference
  }
  $ended = Get-Date
  $output = [string[]]$allOutput.Lines()
  $output | Set-Content -Encoding utf8 -Path $stdoutPath
  "stderr merged into stdout by harness" | Set-Content -Encoding utf8 -Path $stderrPath
  [ordered]@{
    command = $command
    exit_code = $exitCode
    started_at = $started.ToUniversalTime().ToString("o")
    ended_at = $ended.ToUniversalTime().ToString("o")
    latency_ms = [int64]($ended - $started).TotalMilliseconds
  } | ConvertTo-Json -Depth 10 | Set-Content -Encoding utf8 -Path $exitPath

  return [ordered]@{
    exit_code = $exitCode
    latency_ms = [int64]($ended - $started).TotalMilliseconds
    input_path = $inputPath
    stdout_path = $stdoutPath
    stderr_path = $stderrPath
    exit_path = $exitPath
    raw_output = ($output -join "`n")
  }
}

function Get-JsonBlocksFromText {
  param([string]$Text)
  $Text = (($Text -split "`r?`n") | Where-Object { $_ -ne "System.Management.Automation.RemoteException" -and $_ -notmatch "^\s*You> .* INFO " }) -join "`n"
  $blocks = New-Object System.Collections.ArrayList
  $depth = 0
  $inString = $false
  $escaped = $false
  $start = -1

  for ($i = 0; $i -lt $Text.Length; $i++) {
    $ch = $Text[$i]
    if ($inString) {
      if ($escaped) {
        $escaped = $false
      } elseif ($ch -eq '\') {
        $escaped = $true
      } elseif ($ch -eq '"') {
        $inString = $false
      }
      continue
    }

    if ($ch -eq '"') {
      $inString = $true
      continue
    }
    if ($ch -eq '{') {
      if ($depth -eq 0) {
        $lineStart = $Text.LastIndexOf("`n", [Math]::Max(0, $i - 1)) + 1
        if ($Text.Substring($lineStart, $i - $lineStart).Trim().Length -ne 0) { continue }
      }
      if ($depth -eq 0) { $start = $i }
      $depth++
      continue
    }
    if ($ch -eq '}') {
      if ($depth -gt 0) { $depth-- }
      if ($depth -eq 0 -and $start -ge 0) {
        [void]$blocks.Add($Text.Substring($start, $i - $start + 1))
        $start = -1
      }
    }
  }
  return [string[]]$blocks.ToArray()
}

function Parse-VClawChatResponses {
  param([string]$RawOutput, [string]$ArtifactDir)
  $responses = New-Object System.Collections.ArrayList
  foreach ($block in Get-JsonBlocksFromText -Text $RawOutput) {
    try {
      $value = $block | ConvertFrom-Json
      if ($null -ne $value.status -or $null -ne $value.requestId -or $null -ne $value.approvalRequest -or $null -ne $value.toolResults) {
        [void]$responses.Add($value)
      }
    } catch {
      continue
    }
  }
  $responsesPath = Join-Path $ArtifactDir "parsed-responses.json"
  $responseArray = [object[]]$responses.ToArray()
  $responseArray | ConvertTo-Json -Depth 40 | Set-Content -Encoding utf8 -Path $responsesPath
  return [pscustomobject]@{
    responses = $responseArray
    path = $responsesPath
  }
}

function New-Assertion {
  param([string]$Name, [string]$Status, [string]$Details)
  return [pscustomobject]@{ name = $Name; status = $Status; details = $Details }
}

function Get-ActualToolNames {
  param([object[]]$Responses)
  $names = New-Object System.Collections.ArrayList
  foreach ($response in $Responses) {
    foreach ($result in @($response.toolResults)) {
      $toolName = [string]$result.toolName
      if (-not [string]::IsNullOrWhiteSpace($toolName)) { [void]$names.Add($toolName) }
    }
    if ($null -ne $response.approvalRequest -and $null -ne $response.approvalRequest.toolCall) {
      $toolName = [string]$response.approvalRequest.toolCall.toolName
      if (-not [string]::IsNullOrWhiteSpace($toolName)) { [void]$names.Add($toolName) }
    }
  }
  return [string[]]$names.ToArray()
}

function Test-ToolNameMatchesAny {
  param([string]$ToolName, [string[]]$Patterns)
  foreach ($pattern in $Patterns) {
    if ([string]::IsNullOrWhiteSpace($pattern)) { continue }
    if ($ToolName -eq $pattern -or $ToolName -like $pattern) { return $true }
  }
  return $false
}

function Add-ToolSequenceAssertions {
  param([System.Collections.IList]$Assertions, [object]$ScenarioDoc, [object[]]$Responses)
  if ($null -eq $ScenarioDoc.expected_tool_sequence) { return }

  $actualNames = @(Get-ActualToolNames -Responses $Responses)
  $actualJoined = $actualNames -join ","
  $required = @($ScenarioDoc.expected_tool_sequence.required_any_order)
  foreach ($toolName in $required) {
    $status = if ($actualNames -contains [string]$toolName) { "pass" } else { "pending_verification" }
    [void]$Assertions.Add((New-Assertion -Name "expected_tool_observed:$toolName" -Status $status -Details "actual=$actualJoined"))
  }

  $groupIndex = 0
  foreach ($group in @($ScenarioDoc.expected_tool_sequence.required_any_of)) {
    $groupIndex++
    $options = @($group)
    $found = $false
    foreach ($option in $options) {
      if ($actualNames -contains [string]$option) { $found = $true; break }
    }
    [void]$Assertions.Add((New-Assertion -Name "expected_any_of_tool_group:$groupIndex" -Status $(if ($found) { "pass" } else { "pending_verification" }) -Details "expected_any_of=$($options -join ','); actual=$actualJoined"))
  }

  if ($ScenarioDoc.expected_tool_sequence.read_tools_before_write -eq $true) {
    $writePatterns = @($ScenarioDoc.expected_tool_sequence.write_tool_patterns)
    $firstWrite = -1
    for ($i = 0; $i -lt $actualNames.Count; $i++) {
      if (Test-ToolNameMatchesAny -ToolName $actualNames[$i] -Patterns $writePatterns) { $firstWrite = $i; break }
    }
    if ($firstWrite -lt 0) {
      [void]$Assertions.Add((New-Assertion -Name "read_tools_before_write" -Status "pending_verification" -Details "Không thấy write tool trong trace; actual=$actualJoined"))
    } else {
      $readBeforeWrite = $false
      for ($i = 0; $i -lt $firstWrite; $i++) {
        if (-not (Test-ToolNameMatchesAny -ToolName $actualNames[$i] -Patterns $writePatterns)) { $readBeforeWrite = $true; break }
      }
      [void]$Assertions.Add((New-Assertion -Name "read_tools_before_write" -Status $(if ($readBeforeWrite) { "pass" } else { "fail" }) -Details "actual=$actualJoined; first_write_index=$firstWrite"))
    }
  }
}

function Add-ApprovalStatusAssertions {
  param([System.Collections.IList]$Assertions, [object]$ScenarioDoc, [object[]]$Responses)
  if ($null -eq $ScenarioDoc.approval_expectations) { return }

  $approvalResponses = @($Responses | Where-Object { $null -ne $_.approvalRequest })
  $expectation = [string]$ScenarioDoc.approval_expectations.approval_required_for_write
  switch ($expectation) {
    "required" {
      [void]$Assertions.Add((New-Assertion -Name "approval_required_observed" -Status $(if ($approvalResponses.Count -gt 0) { "pass" } else { "fail" }) -Details "approval_responses=$($approvalResponses.Count)"))
    }
    "policy_dependent" {
      [void]$Assertions.Add((New-Assertion -Name "approval_policy_dependent_observed_or_noted" -Status $(if ($approvalResponses.Count -gt 0) { "pass" } else { "pending_verification" }) -Details "approval_responses=$($approvalResponses.Count); policy may auto-allow"))
    }
  }

  if ($ScenarioDoc.approval_expectations.scripted_decisions_must_be_observed -eq $true) {
    $scripted = @($ScenarioDoc.scripted_decisions | ForEach-Object { [string]$_.decision } | Where-Object { -not [string]::IsNullOrWhiteSpace($_) })
    $hasScriptedApproval = ($scripted | Where-Object { @("approve", "reject", "revise", "cancel", "duplicate") -contains $_ }).Count -gt 0
    if ($hasScriptedApproval) {
      [void]$Assertions.Add((New-Assertion -Name "scripted_approval_has_runtime_request" -Status $(if ($approvalResponses.Count -gt 0) { "pass" } else { "pending_verification" }) -Details "scripted=$($scripted -join ','); approval_responses=$($approvalResponses.Count)"))
    }
  }

  $allowed = @($ScenarioDoc.approval_expectations.allowed_final_statuses)
  if ($allowed.Count -gt 0 -and $Responses.Count -gt 0) {
    $lastStatus = [string]$Responses[$Responses.Count - 1].status
    [void]$Assertions.Add((New-Assertion -Name "final_status_allowed" -Status $(if ($allowed -contains $lastStatus) { "pass" } else { "fail" }) -Details "final_status=$lastStatus; allowed=$($allowed -join ',')"))
  }
}

function Add-ResponseContentAssertions {
  param([System.Collections.IList]$Assertions, [object]$ScenarioDoc, [object[]]$Responses)
  if ($null -eq $ScenarioDoc.response_expectations) { return }
  $messages = @($Responses | ForEach-Object { [string]$_.message })
  $joined = ($messages -join "`n")
  foreach ($needle in @($ScenarioDoc.response_expectations.required_substrings)) {
    if ([string]::IsNullOrWhiteSpace([string]$needle)) { continue }
    [void]$Assertions.Add((New-Assertion -Name "response_contains:$needle" -Status $(if ($joined -like "*$needle*") { "pass" } else { "pending_verification" }) -Details "required=$needle"))
  }
}
function Build-ChatHardAssertions {
  param(
    [object]$ScenarioDoc,
    [string]$RunId,
    [object]$Chat,
    [object[]]$Responses
  )
  $assertions = New-Object System.Collections.ArrayList
  [void]$assertions.Add((New-Assertion -Name "vclaw_chat_process_exit_zero" -Status $(if ($Chat.exit_code -eq 0) { "pass" } else { "fail" }) -Details "exit_code=$($Chat.exit_code)"))
  [void]$assertions.Add((New-Assertion -Name "scripted_input_written" -Status "pass" -Details $Chat.input_path))
  [void]$assertions.Add((New-Assertion -Name "chat_output_captured" -Status "pass" -Details $Chat.stdout_path))
  [void]$assertions.Add((New-Assertion -Name "response_json_parsed" -Status $(if ($Responses.Count -gt 0) { "pass" } else { "fail" }) -Details "responses=$($Responses.Count)"))

  $inputText = Get-Content -Raw -Path $Chat.input_path
  [void]$assertions.Add((New-Assertion -Name "scripted_input_contains_run_id" -Status $(if ($inputText.Contains($RunId)) { "pass" } else { "fail" }) -Details "run_id=$RunId"))

  $hasApprovalDecision = $false
  foreach ($decision in @($ScenarioDoc.scripted_decisions)) {
    if (@("approve", "reject", "revise", "cancel", "duplicate") -contains [string]$decision.decision) {
      $hasApprovalDecision = $true
    }
  }
  if ($hasApprovalDecision) {
    $approvalResponses = @($Responses | Where-Object { $null -ne $_.approvalRequest })
    [void]$assertions.Add((New-Assertion -Name "approval_request_observed_when_scripted" -Status $(if ($approvalResponses.Count -gt 0) { "pass" } else { "pending_verification" }) -Details "approval_responses=$($approvalResponses.Count)"))
  }

  $toolResultCount = 0
  foreach ($response in $Responses) {
    if ($null -ne $response.toolResults) { $toolResultCount += @($response.toolResults).Count }
  }
  $toolTraceStatus = if ($toolResultCount -gt 0 -or (Get-WriteExpectation -ScenarioDoc $ScenarioDoc) -eq "none") { "pass" } else { "pending_verification" }
  [void]$assertions.Add((New-Assertion -Name "tool_results_trace_available" -Status $toolTraceStatus -Details "tool_results=$toolResultCount"))

  Add-ToolSequenceAssertions -Assertions $assertions -ScenarioDoc $ScenarioDoc -Responses $Responses
  Add-ApprovalStatusAssertions -Assertions $assertions -ScenarioDoc $ScenarioDoc -Responses $Responses
  Add-ResponseContentAssertions -Assertions $assertions -ScenarioDoc $ScenarioDoc -Responses $Responses

  return [object[]]$assertions.ToArray()
}

function Extract-ResponseTrace {
  param([object[]]$Responses)
  $toolCalls = New-Object System.Collections.ArrayList
  $approvals = New-Object System.Collections.ArrayList
  $objectsWritten = New-Object System.Collections.ArrayList
  $toolCallIDs = New-Object System.Collections.ArrayList
  $approvalIDs = New-Object System.Collections.ArrayList

  foreach ($response in $Responses) {
    if ($null -ne $response.approvalRequest) {
      $approval = $response.approvalRequest
      [void]$approvals.Add([pscustomobject]@{
        approval_id = $approval.approvalId
        status = $approval.status
        tool_call_id = $approval.toolCallId
        tool_name = if ($null -ne $approval.toolCall) { $approval.toolCall.toolName } else { "" }
        risk_level = $approval.riskLevel
      })
      if ($approval.approvalId) { [void]$approvalIDs.Add([string]$approval.approvalId) }
      if ($approval.toolCallId) { [void]$toolCallIDs.Add([string]$approval.toolCallId) }
    }
    foreach ($result in @($response.toolResults)) {
      [void]$toolCalls.Add([pscustomobject]@{
        tool_call_id = $result.toolCallId
        tool_name = $result.toolName
        success = $result.success
        error_code = if ($null -ne $result.error) { $result.error.code } else { "" }
      })
      if ($result.toolCallId) { [void]$toolCallIDs.Add([string]$result.toolCallId) }
      $artifactId = if ($null -ne $result.artifactRef) { [string]$result.artifactRef.id } else { "" }
      if ($result.success -eq $true -and $null -ne $result.artifactRef -and -not [string]::IsNullOrWhiteSpace($artifactId)) {
        [void]$objectsWritten.Add([pscustomobject]@{
          object_id = $artifactId
          kind = $result.artifactRef.kind
          label = $result.artifactRef.label
          uri = $result.artifactRef.uri
          tool_name = $result.toolName
          tool_call_id = $result.toolCallId
          run_id = ""
          run_id_present = $false
          cleanup_supported = $false
        })
      } elseif ($result.success -eq $true -and [string]$result.toolName -eq "gmail.createDraft") {
        $draftId = ""
        try {
          $content = [string]$result.data.contentForLLM
          if (-not [string]::IsNullOrWhiteSpace($content)) {
            $contentJson = $content | ConvertFrom-Json
            if ($null -ne $contentJson.Draft -and -not [string]::IsNullOrWhiteSpace([string]$contentJson.Draft.ID)) {
              $draftId = [string]$contentJson.Draft.ID
            }
          }
        } catch {
          $draftId = ""
        }
        if (-not [string]::IsNullOrWhiteSpace($draftId)) {
          [void]$objectsWritten.Add([pscustomobject]@{
            object_id = $draftId
            kind = "google.gmail.draft"
            label = "gmail draft"
            uri = ""
            tool_name = $result.toolName
            tool_call_id = $result.toolCallId
            run_id = ""
            run_id_present = $false
            cleanup_supported = $false
          })
        }
      }
    }
  }

  return [pscustomobject]@{
    tool_calls = [object[]]$toolCalls.ToArray()
    approvals = [object[]]$approvals.ToArray()
    objects_written = [object[]]$objectsWritten.ToArray()
    approval_ids = [string[]]@($approvalIDs.ToArray() | Select-Object -Unique)
    tool_call_ids = [string[]]@($toolCallIDs.ToArray() | Select-Object -Unique)
  }
}

function Invoke-E2ECommand {
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
  return [ordered]@{ exit_code = $exitCode; output = $text; stdout_path = $stdoutPath; meta_path = $metaPath; latency_ms = [int64]($ended - $started).TotalMilliseconds }
}

function Test-OutputContainsRunId {
  param([string]$Output, [string]$RunId)
  return (-not [string]::IsNullOrWhiteSpace($Output)) -and $Output.Contains($RunId)
}

function Invoke-ObjectReadBack {
  param([object[]]$Objects, [string]$RunId, [string]$ArtifactDir, [string]$CommandLine)
  $assertions = New-Object System.Collections.ArrayList
  $updatedObjects = New-Object System.Collections.ArrayList
  $index = 0
  foreach ($object in @($Objects)) {
    $index++
    $kind = [string]$object.kind
    $id = [string]$object.object_id
    $toolName = [string]$object.tool_name
    $cmd = ""
    $cleanupCmd = ""
    $cleanupSupported = $false

    switch ($kind) {
      { $_ -eq "google.chat.message" -or $_ -eq "chat.message" } {
        if (-not [string]::IsNullOrWhiteSpace($id)) {
          $space = $env:VCLAW_E2E_CHAT_SPACE
          if ($id -match '^(spaces/[^/]+)/messages/') { $space = $Matches[1] }
          if ([string]::IsNullOrWhiteSpace($space)) {
            [void]$assertions.Add((New-Assertion -Name "read_back_chat_message_env:$index" -Status "blocked_env" -Details "VCLAW_E2E_CHAT_SPACE is required to list messages for read-back"))
          } else {
            $cmd = "$CommandLine google chat list-messages -space $space -max-results 50"
            $cleanupCmd = "$CommandLine google chat delete-message -name $id"
            $cleanupSupported = $true
          }
        }
      }
      "google.drive.file" {
        if (-not [string]::IsNullOrWhiteSpace($id)) {
          $cmd = "$CommandLine google drive get -id $id"
          $cleanupCmd = "$CommandLine google drive trash -id $id"
          $cleanupSupported = $true
        }
      }
      "google.drive.folder" {
        if (-not [string]::IsNullOrWhiteSpace($id)) {
          $cmd = "$CommandLine google drive get -id $id"
          $cleanupCmd = "$CommandLine google drive trash -id $id"
          $cleanupSupported = $true
        }
      }
      "google.docs.document" {
        if (-not [string]::IsNullOrWhiteSpace($id)) {
          $cmd = "$CommandLine google docs get -id $id -full"
          $cleanupCmd = "$CommandLine google drive trash -id $id"
          $cleanupSupported = $true
        }
      }
      "google.sheets.spreadsheet" {
        if (-not [string]::IsNullOrWhiteSpace($id)) {
          $cmd = "$CommandLine google sheets get -id $id"
          $cleanupCmd = "$CommandLine google drive trash -id $id"
          $cleanupSupported = $true
        }
      }
      "google.gmail.draft" {
        if (-not [string]::IsNullOrWhiteSpace($id)) {
          $cmd = "$CommandLine google gmail get-draft -id $id -full"
          $cleanupCmd = "$CommandLine google gmail delete-draft -id $id"
          $cleanupSupported = $true
        }
      }
      "google.gmail.message" {
        if (-not [string]::IsNullOrWhiteSpace($id)) {
          $cmd = "$CommandLine google gmail get -id $id -full"
        }
      }
    }

    if ([string]::IsNullOrWhiteSpace($cmd)) {
      [void]$assertions.Add((New-Assertion -Name "read_back_supported:$index" -Status "pending_verification" -Details "kind=$kind id=$id tool=$toolName has no read-back command yet"))
      $object.cleanup_supported = $cleanupSupported
      [void]$updatedObjects.Add($object)
      continue
    }

    $read = Invoke-E2ECommand -CommandLine $cmd -ArtifactDir $ArtifactDir -Name "readback_$index`_$kind"
    $containsRunID = Test-OutputContainsRunId -Output $read.output -RunId $RunId
    [void]$assertions.Add((New-Assertion -Name "object_read_back:$index" -Status $(if ($read.exit_code -eq 0 -and $containsRunID) { "pass" } else { "fail" }) -Details "kind=$kind id=$id exit=$($read.exit_code) contains_run_id=$containsRunID stdout=$($read.stdout_path)"))
    $object.run_id = $RunId
    $object.run_id_present = $containsRunID
    $object.cleanup_supported = $cleanupSupported
    $object | Add-Member -NotePropertyName read_back_stdout -NotePropertyValue $read.stdout_path -Force
    [void]$updatedObjects.Add($object)
  }
  return [pscustomobject]@{ assertions = [object[]]$assertions.ToArray(); objects = [object[]]$updatedObjects.ToArray() }
}

function Invoke-ObjectCleanup {
  param([object[]]$Objects, [string]$RunId, [string]$ArtifactDir, [string]$CommandLine)
  $assertions = New-Object System.Collections.ArrayList
  $cleaned = New-Object System.Collections.ArrayList
  $index = 0
  foreach ($object in @($Objects)) {
    $index++
    if ($object.run_id_present -ne $true) {
      [void]$assertions.Add((New-Assertion -Name "cleanup_guard_run_id_present:$index" -Status "fail" -Details "Refuse cleanup without read-back run_id proof: kind=$($object.kind) id=$($object.object_id)"))
      continue
    }
    if ($object.cleanup_supported -ne $true) {
      [void]$assertions.Add((New-Assertion -Name "cleanup_supported:$index" -Status "pending_verification" -Details "No cleanup command for kind=$($object.kind) id=$($object.object_id)"))
      continue
    }

    $cmd = ""
    switch ([string]$object.kind) {
      "google.chat.message" { $cmd = "$CommandLine google chat delete-message -name $($object.object_id)" }
      "chat.message" { $cmd = "$CommandLine google chat delete-message -name $($object.object_id)" }
      "google.drive.file" { $cmd = "$CommandLine google drive trash -id $($object.object_id)" }
      "google.drive.folder" { $cmd = "$CommandLine google drive trash -id $($object.object_id)" }
      "google.gmail.draft" { $cmd = "$CommandLine google gmail delete-draft -id $($object.object_id)" }
      "google.docs.document" { $cmd = "$CommandLine google drive trash -id $($object.object_id)" }
      "google.sheets.spreadsheet" { $cmd = "$CommandLine google drive trash -id $($object.object_id)" }
    }
    if ([string]::IsNullOrWhiteSpace($cmd)) {
      [void]$assertions.Add((New-Assertion -Name "cleanup_command_exists:$index" -Status "pending_verification" -Details "No cleanup command for kind=$($object.kind)"))
      continue
    }
    $cleanup = Invoke-E2ECommand -CommandLine $cmd -ArtifactDir $ArtifactDir -Name "cleanup_$index`_$($object.kind)"
    [void]$assertions.Add((New-Assertion -Name "object_cleanup:$index" -Status $(if ($cleanup.exit_code -eq 0) { "pass" } else { "fail" }) -Details "kind=$($object.kind) id=$($object.object_id) exit=$($cleanup.exit_code) stdout=$($cleanup.stdout_path)"))
    if ($cleanup.exit_code -eq 0) { [void]$cleaned.Add($object) }
  }
  return [pscustomobject]@{ assertions = [object[]]$assertions.ToArray(); cleaned = [object[]]$cleaned.ToArray() }
}

function Test-IsWriteToolName {
  param([string]$ToolName, [object]$ScenarioDoc)
  $patterns = @()
  if ($null -ne $ScenarioDoc.expected_tool_sequence) {
    $patterns = @($ScenarioDoc.expected_tool_sequence.write_tool_patterns)
  }
  if ($patterns.Count -eq 0) {
    $patterns = @("gmail.createDraft", "gmail.sendDraft", "chat.sendMessage", "calendar.createEvent", "calendar.updateEvent", "calendar.deleteEvent", "docs.createDocument", "docs.appendText", "drive.createFile", "drive.uploadFile")
  }
  return Test-ToolNameMatchesAny -ToolName $ToolName -Patterns $patterns
}

function Get-WriteExpectation {
  param([object]$ScenarioDoc)
  if ($null -ne $ScenarioDoc.write_expectation -and -not [string]::IsNullOrWhiteSpace([string]$ScenarioDoc.write_expectation)) {
    return [string]$ScenarioDoc.write_expectation
  }
  return "required"
}

function Add-RequiredWriteAssertions {
  param(
    [System.Collections.IList]$Assertions,
    [object[]]$Objects,
    [object[]]$Responses,
    [object]$ScenarioDoc
  )
  $writePatterns = @()
  if ($null -ne $ScenarioDoc.expected_tool_sequence) {
    $writePatterns = @($ScenarioDoc.expected_tool_sequence.write_tool_patterns)
  }
  if ($writePatterns.Count -eq 0) { return }

  $successfulWriteResults = New-Object System.Collections.ArrayList
  foreach ($response in @($Responses)) {
    foreach ($result in @($response.toolResults)) {
      $toolName = [string]$result.toolName
      if ($result.success -eq $true -and (Test-ToolNameMatchesAny -ToolName $toolName -Patterns $writePatterns)) {
        [void]$successfulWriteResults.Add($result)
      }
    }
  }

  $writeExpectation = Get-WriteExpectation -ScenarioDoc $ScenarioDoc
  if ($writeExpectation -eq "none") {
    [void]$Assertions.Add((New-Assertion -Name "no_write_tool_executed" -Status $(if ($successfulWriteResults.Count -eq 0) { "pass" } else { "fail" }) -Details "successful_write_results=$($successfulWriteResults.Count); patterns=$($writePatterns -join ',')"))
    [void]$Assertions.Add((New-Assertion -Name "no_object_written" -Status $(if ($Objects.Count -eq 0) { "pass" } else { "fail" }) -Details "objects=$($Objects.Count)"))
    return
  }

  $minWrites = 1
  if ($null -ne $ScenarioDoc.write_expectations -and $null -ne $ScenarioDoc.write_expectations.min_successful_write_results) {
    $minWrites = [int]$ScenarioDoc.write_expectations.min_successful_write_results
  }
  $minObjects = 1
  if ($null -ne $ScenarioDoc.write_expectations -and $null -ne $ScenarioDoc.write_expectations.min_objects_written) {
    $minObjects = [int]$ScenarioDoc.write_expectations.min_objects_written
  }
  [void]$Assertions.Add((New-Assertion -Name "write_tool_executed" -Status $(if ($successfulWriteResults.Count -ge $minWrites) { "pass" } else { "pending_verification" }) -Details "successful_write_results=$($successfulWriteResults.Count); min=$minWrites; patterns=$($writePatterns -join ',')"))
  [void]$Assertions.Add((New-Assertion -Name "objects_written_minimum" -Status $(if ($Objects.Count -ge $minObjects) { "pass" } else { "pending_verification" }) -Details "objects=$($Objects.Count); min=$minObjects"))
  [void]$Assertions.Add((New-Assertion -Name "write_object_contains_run_id" -Status $(if ($Objects.Count -gt 0 -and @($Objects | Where-Object { $_.run_id_present -eq $true }).Count -eq $Objects.Count) { "pass" } else { "pending_verification" }) -Details "objects=$($Objects.Count); with_run_id=$(@($Objects | Where-Object { $_.run_id_present -eq $true }).Count)"))
  [void]$Assertions.Add((New-Assertion -Name "object_read_back_matches_final_content" -Status $(if ($Objects.Count -gt 0 -and @($Objects | Where-Object { $_.run_id_present -eq $true }).Count -eq $Objects.Count) { "pass" } else { "pending_verification" }) -Details "objects=$($Objects.Count)"))
}

function Add-IdempotencyAssertions {
  param(
    [System.Collections.IList]$Assertions,
    [object[]]$Objects,
    [object[]]$Responses,
    [object]$ScenarioDoc
  )
  $seen = @{}
  foreach ($object in @($Objects)) {
    $key = "$($object.kind)|$($object.object_id)"
    if ([string]::IsNullOrWhiteSpace($object.object_id)) { continue }
    if ($seen.ContainsKey($key)) {
      [void]$Assertions.Add((New-Assertion -Name "no_duplicate_side_effect:$key" -Status "fail" -Details "Duplicate object id in one run: $key"))
    } else {
      $seen[$key] = $true
    }
  }
  if ($Objects.Count -gt 0) {
    [void]$Assertions.Add((New-Assertion -Name "no_duplicate_side_effect_summary" -Status "pass" -Details "unique_objects=$($seen.Count); total_objects=$($Objects.Count)"))
  } elseif ((Get-WriteExpectation -ScenarioDoc $ScenarioDoc) -eq "none") {
    [void]$Assertions.Add((New-Assertion -Name "no_duplicate_side_effect_summary" -Status "pass" -Details "No written objects expected or observed."))
  } else {
    [void]$Assertions.Add((New-Assertion -Name "no_duplicate_side_effect_summary" -Status "pending_verification" -Details "No written objects were extracted from artifact refs."))
  }

  if ($null -eq $ScenarioDoc.idempotency_expectations -or $ScenarioDoc.idempotency_expectations.duplicate_decision -ne $true) {
    return
  }

  $successfulWriteResults = New-Object System.Collections.ArrayList
  $approvalNotFoundResponses = New-Object System.Collections.ArrayList
  foreach ($response in @($Responses)) {
    if ($null -ne $response.error) {
      $code = [string]$response.error.code
      $message = [string]$response.error.message
      if ($code -eq "APPROVAL_NOT_FOUND" -or $message -match "(?i)approval.*not found|pending approval not found|Không tìm thấy yêu cầu xác nhận") {
        [void]$approvalNotFoundResponses.Add($response)
      }
    }
    foreach ($result in @($response.toolResults)) {
      $toolName = [string]$result.toolName
      if ($result.success -eq $true -and (Test-IsWriteToolName -ToolName $toolName -ScenarioDoc $ScenarioDoc)) {
        [void]$successfulWriteResults.Add($result)
      }
    }
  }

  $maxWrites = 1
  if ($null -ne $ScenarioDoc.idempotency_expectations.max_successful_write_results) {
    $maxWrites = [int]$ScenarioDoc.idempotency_expectations.max_successful_write_results
  }
  [void]$Assertions.Add((New-Assertion -Name "duplicate_approval_max_successful_write_results" -Status $(if ($successfulWriteResults.Count -le $maxWrites) { "pass" } else { "fail" }) -Details "successful_write_results=$($successfulWriteResults.Count); max=$maxWrites"))

  if ($ScenarioDoc.idempotency_expectations.duplicate_approval_not_found_is_ok -eq $true) {
    $hasDuplicateCommand = $false
    foreach ($decision in @($ScenarioDoc.scripted_decisions)) {
      if ([string]$decision.decision -eq "duplicate") { $hasDuplicateCommand = $true }
    }
    if ($hasDuplicateCommand) {
      $status = if ($approvalNotFoundResponses.Count -gt 0 -or $successfulWriteResults.Count -le $maxWrites) { "pass" } else { "fail" }
      [void]$Assertions.Add((New-Assertion -Name "duplicate_approval_does_not_execute_again" -Status $status -Details "approval_not_found_responses=$($approvalNotFoundResponses.Count); successful_write_results=$($successfulWriteResults.Count)"))
    }
  }
}

function Invoke-PostgresScalar {
  param([string]$Sql, [string]$ArtifactDir, [string]$Name)
  $databaseURL = [Environment]::GetEnvironmentVariable("DATABASE_URL")
  if ([string]::IsNullOrWhiteSpace($databaseURL)) {
    return [ordered]@{ status = "blocked_env"; value = $null; details = "DATABASE_URL is required"; stdout_path = "" }
  }
  $safeName = ($Name -replace '[^A-Za-z0-9_.-]', '_')
  $stdoutPath = Join-Path $ArtifactDir "$safeName.psql.txt"
  $command = "psql `"$databaseURL`" -X -t -A -c `"$Sql`""
  $output = & cmd.exe /c $command 2>&1
  $exitCode = $LASTEXITCODE
  $text = ($output -join "`n").Trim()
  $text | Set-Content -Encoding utf8 -Path $stdoutPath
  if ($exitCode -ne 0) {
    return [ordered]@{ status = "blocked_env"; value = $null; details = "psql exit_code=$exitCode"; stdout_path = $stdoutPath }
  }
  return [ordered]@{ status = "pass"; value = $text; details = "psql ok"; stdout_path = $stdoutPath }
}

function Add-PostgresIdempotencyAssertions {
  param(
    [System.Collections.IList]$Assertions,
    [object]$ScenarioDoc,
    [string]$RunId,
    [string]$ArtifactDir
  )
  if ($null -eq $ScenarioDoc.idempotency_expectations -or $ScenarioDoc.idempotency_expectations.duplicate_decision -ne $true) {
    return
  }

  $sessionID = "e2e-$RunId"
  $maxWrites = 1
  if ($null -ne $ScenarioDoc.idempotency_expectations.max_successful_write_results) {
    $maxWrites = [int]$ScenarioDoc.idempotency_expectations.max_successful_write_results
  }
  $writeToolsSql = "'gmail.createDraft','gmail.sendDraft','chat.sendMessage','calendar.createEvent','calendar.updateEvent','calendar.deleteEvent','docs.createDocument','docs.appendText','drive.createFile','drive.uploadFile'"

  $completedActions = Invoke-PostgresScalar -ArtifactDir $ArtifactDir -Name "pg_completed_actions" -Sql "select count(*) from approval_actions where session_id = '$sessionID' and status = 'completed';"
  if ($completedActions.status -ne "pass") {
    [void]$Assertions.Add((New-Assertion -Name "postgres_completed_actions_query" -Status $completedActions.status -Details "$($completedActions.details); stdout=$($completedActions.stdout_path)"))
    return
  }
  $completedCount = [int]$completedActions.value
  [void]$Assertions.Add((New-Assertion -Name "postgres_completed_actions_max_once" -Status $(if ($completedCount -le $maxWrites) { "pass" } else { "fail" }) -Details "completed_actions=$completedCount; max=$maxWrites; stdout=$($completedActions.stdout_path)"))

  $completedToolCalls = Invoke-PostgresScalar -ArtifactDir $ArtifactDir -Name "pg_completed_write_tool_calls" -Sql "select count(*) from tool_calls where session_id = '$sessionID' and status = 'completed' and tool_name in ($writeToolsSql);"
  if ($completedToolCalls.status -ne "pass") {
    [void]$Assertions.Add((New-Assertion -Name "postgres_completed_write_tool_calls_query" -Status $completedToolCalls.status -Details "$($completedToolCalls.details); stdout=$($completedToolCalls.stdout_path)"))
    return
  }
  $toolCallCount = [int]$completedToolCalls.value
  [void]$Assertions.Add((New-Assertion -Name "postgres_completed_write_tool_calls_max_once" -Status $(if ($toolCallCount -le $maxWrites) { "pass" } else { "fail" }) -Details "completed_write_tool_calls=$toolCallCount; max=$maxWrites; stdout=$($completedToolCalls.stdout_path)"))

  $duplicateActionKeys = Invoke-PostgresScalar -ArtifactDir $ArtifactDir -Name "pg_duplicate_idempotency_keys" -Sql "select count(*) from (select idempotency_key from approval_actions where session_id = '$sessionID' and coalesce(idempotency_key, '') <> '' group by idempotency_key having count(*) > 1) d;"
  if ($duplicateActionKeys.status -ne "pass") {
    [void]$Assertions.Add((New-Assertion -Name "postgres_duplicate_idempotency_key_query" -Status $duplicateActionKeys.status -Details "$($duplicateActionKeys.details); stdout=$($duplicateActionKeys.stdout_path)"))
    return
  }
  $duplicateKeyCount = [int]$duplicateActionKeys.value
  [void]$Assertions.Add((New-Assertion -Name "postgres_no_duplicate_idempotency_keys" -Status $(if ($duplicateKeyCount -eq 0) { "pass" } else { "fail" }) -Details "duplicate_idempotency_keys=$duplicateKeyCount; stdout=$($duplicateActionKeys.stdout_path)"))
}

$scenarioPath = Join-Path $RepoRoot (Join-Path "testing-e2e/scenarios" "$Scenario.json")
if (-not (Test-Path $scenarioPath)) {
  throw "Không tìm thấy scenario: $scenarioPath"
}

$scenarioDoc = Get-Content -Raw -Path $scenarioPath | ConvertFrom-Json
$runId = New-RunId
$artifactDir = Join-Path $OutDir $runId
New-Item -ItemType Directory -Force -Path $artifactDir | Out-Null

$requiredEnv = @($scenarioDoc.fixtures.required_env)
$missing = @()
foreach ($name in $requiredEnv) {
  if ([string]::IsNullOrWhiteSpace([Environment]::GetEnvironmentVariable($name))) {
    $missing += $name
  }
}

if ($missing.Count -gt 0) {
  $summary = New-BaseSummary -ScenarioDoc $scenarioDoc -RunId $runId -Status "blocked_env" -Reason "Thiếu env/selector bắt buộc: $($missing -join ', ')" -ReadinessCounted $false -ArtifactDir $artifactDir -MissingEnv $missing
  $summaryPath = Save-Summary -Summary $summary -ArtifactDir $artifactDir
  "blocked_env: $($missing -join ', ')"
  "summary: $summaryPath"
  exit 2
}

if ($DryRun) {
  $summary = New-BaseSummary -ScenarioDoc $scenarioDoc -RunId $runId -Status "pending_verification" -Reason "DryRun chỉ kiểm tra scenario/env, chưa chạy V-Claw thật." -ReadinessCounted $false -ArtifactDir $artifactDir
  $summary.expected_tool_calls = @($scenarioDoc.hard_assertions)
  $summaryPath = Save-Summary -Summary $summary -ArtifactDir $artifactDir
  "pending_verification: DryRun hoàn tất, chưa chạy real E2E"
  "summary: $summaryPath"
  exit 0
}

if ($RunChat) {
  $summary = New-BaseSummary -ScenarioDoc $scenarioDoc -RunId $runId -Status "pending_verification" -Reason "Đã chạy scripted chat, parse stdout, read-back/cleanup/idempotency assertions." -ReadinessCounted $false -ArtifactDir $artifactDir
  $chat = Invoke-VClawChatScript -ScenarioDoc $scenarioDoc -RunId $runId -ArtifactDir $artifactDir -CommandLine $VClawCommand
  $parsed = Parse-VClawChatResponses -RawOutput $chat.raw_output -ArtifactDir $artifactDir
  $trace = Extract-ResponseTrace -Responses $parsed.responses
  $assertions = New-Object System.Collections.ArrayList
  foreach ($assertion in @(Build-ChatHardAssertions -ScenarioDoc $scenarioDoc -RunId $runId -Chat $chat -Responses $parsed.responses)) {
    [void]$assertions.Add($assertion)
  }

  $readBack = Invoke-ObjectReadBack -Objects $trace.objects_written -RunId $runId -ArtifactDir $artifactDir -CommandLine $VClawCommand
  foreach ($assertion in @($readBack.assertions)) { [void]$assertions.Add($assertion) }

  Add-RequiredWriteAssertions -Assertions $assertions -Objects $readBack.objects -Responses $parsed.responses -ScenarioDoc $scenarioDoc
  Add-IdempotencyAssertions -Assertions $assertions -Objects $readBack.objects -Responses $parsed.responses -ScenarioDoc $scenarioDoc
  if (@($scenarioDoc.fixtures.required_env) -contains "DATABASE_URL") {
    Add-PostgresIdempotencyAssertions -Assertions $assertions -ScenarioDoc $scenarioDoc -RunId $runId -ArtifactDir $artifactDir
  }

  $cleanup = Invoke-ObjectCleanup -Objects $readBack.objects -RunId $runId -ArtifactDir $artifactDir -CommandLine $VClawCommand
  foreach ($assertion in @($cleanup.assertions)) { [void]$assertions.Add($assertion) }

  $summary.latency_ms = $chat.latency_ms
  $summary.expected_tool_calls = @($scenarioDoc.hard_assertions)
  $summary.actual_tool_calls = @($trace.tool_calls)
  $summary.approvals = @($trace.approvals)
  $summary.objects_written = @($readBack.objects)
  $summary.read_back_assertions = @($readBack.assertions)
  $summary.idempotency_assertions = @($assertions | Where-Object { ([string]$_.name).StartsWith("no_duplicate_side_effect") })
  $summary.cleanup = [ordered]@{
    attempted = ($readBack.objects.Count -gt 0)
    status = if (@($cleanup.assertions | Where-Object { $_.status -eq "fail" }).Count -gt 0) { "fail" } elseif (@($cleanup.assertions | Where-Object { $_.status -eq "pending_verification" -or $_.status -eq "blocked_env" }).Count -gt 0) { "pending_verification" } elseif ($readBack.objects.Count -gt 0) { "pass" } else { "pending_verification" }
    objects_deleted_or_reverted = @($cleanup.cleaned)
  }
  $summary.trace.session_id = "e2e-$runId"
  $summary.trace.approval_ids = @($trace.approval_ids)
  $summary.trace.tool_call_ids = @($trace.tool_call_ids)
  if ((Get-WriteExpectation -ScenarioDoc $scenarioDoc) -eq "none" -and $readBack.objects.Count -eq 0) {
    [void]$assertions.Add((New-Assertion -Name "cleanup_not_needed" -Status "pass" -Details "No objects expected or observed for cleanup."))
    $summary.cleanup.status = "pass"
  } elseif ($summary.cleanup.status -ne "pass") {
    [void]$assertions.Add((New-Assertion -Name "cleanup_attempted" -Status "pending_verification" -Details "cleanup_status=$($summary.cleanup.status); attempted=$($summary.cleanup.attempted)"))
  } else {
    [void]$assertions.Add((New-Assertion -Name "cleanup_attempted" -Status "pass" -Details "cleanup_status=$($summary.cleanup.status); cleaned=$(@($summary.cleanup.objects_deleted_or_reverted).Count)"))
  }
  $summary.hard_assertions = @($assertions)

  $failedAssertions = @($assertions | Where-Object { $_.status -eq "fail" })
  if ($chat.exit_code -ne 0 -or $failedAssertions.Count -gt 0) {
    $summary.status = "fail"
    $summary.status_reason = "Một hoặc nhiều hard assertions fail. Xem parsed-responses/read-back/cleanup artifacts."
    $summary.readiness_counted = $false
  } else {
    $pendingAssertions = @($assertions | Where-Object { $_.status -eq "pending_verification" -or $_.status -eq "blocked_env" })
    if ($pendingAssertions.Count -gt 0) {
      $summary.status = "pending_verification"
      $summary.status_reason = "Không có assertion fail, nhưng còn assertion pending/blocked nên chưa được tính readiness."
      $summary.readiness_counted = $false
    } else {
      $summary.status = "pass"
      $summary.status_reason = "Tất cả hard assertions, read-back, cleanup và idempotency assertions đều pass."
      $summary.readiness_counted = $true
    }
  }

  $summaryPath = Save-Summary -Summary $summary -ArtifactDir $artifactDir
  "status: $($summary.status)"
  "summary: $summaryPath"
  if ($chat.exit_code -ne 0) { exit $chat.exit_code }
  if ($summary.status -eq "pass") { exit 0 }
  exit 1
}

$summary = New-BaseSummary -ScenarioDoc $scenarioDoc -RunId $runId -Status "pending_verification" -Reason "Chưa truyền -RunChat nên harness chưa chạy V-Claw runtime thật." -ReadinessCounted $false -ArtifactDir $artifactDir
$summary.expected_tool_calls = @($scenarioDoc.hard_assertions)
$summaryPath = Save-Summary -Summary $summary -ArtifactDir $artifactDir
"pending_verification: thêm -RunChat để chạy vclaw agent chat scripted approval"
"summary: $summaryPath"
exit 0



























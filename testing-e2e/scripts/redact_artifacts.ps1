param(
  [Parameter(Mandatory=$true)]
  [string]$ArtifactDir
)

$ErrorActionPreference = "Stop"

if (-not (Test-Path $ArtifactDir)) {
  throw "Không tìm thấy ArtifactDir: $ArtifactDir"
}

$patterns = @(
  @{ Name = "email"; Pattern = "[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}"; Replacement = "[REDACTED_EMAIL]" },
  @{ Name = "token_like"; Pattern = "(?i)(api[_-]?key|token|secret|authorization)\s*[:=]\s*[^\s,}\"]+"; Replacement = "[REDACTED_SECRET]" }
)

Get-ChildItem -Path $ArtifactDir -Recurse -File | ForEach-Object {
  $path = $_.FullName
  $content = Get-Content -Raw -Path $path
  foreach ($rule in $patterns) {
    $content = [regex]::Replace($content, $rule.Pattern, $rule.Replacement)
  }
  Set-Content -Encoding utf8 -Path $path -Value $content
}

"redacted: $ArtifactDir"

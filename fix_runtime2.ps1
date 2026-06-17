$lines = Get-Content internal/agent/runtime.go
$t = [char]9
$newLines = New-Object System.Collections.Generic.List[string]
for ($i = 0; $i -lt $lines.Length; $i++) {
    $newLines.Add($lines[$i])
    if ($i -eq 424) {
        $newLines.Add($t + $t + 'if resp := r.handleContextError(ctx, runState, toolResults); resp != nil {')
        $newLines.Add($t + $t + $t + 'resp.RequestID = message.RequestID')
        $newLines.Add($t + $t + $t + 'resp.SessionID = message.SessionID')
        $newLines.Add($t + $t + $t + 'return *resp, nil')
        $newLines.Add($t + $t + '}')
    }
}
[System.IO.File]::WriteAllLines("D:\Vinsmart\n2task4\V-Claw\internal\agent\runtime.go", $newLines)

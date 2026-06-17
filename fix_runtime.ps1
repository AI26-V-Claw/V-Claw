$lines = Get-Content internal/agent/runtime.go
$before = $lines[0..970]
$after = $lines[997..($lines.Length-1)]
$t = [char]9
$newFunc = @(
    'func (r *Runtime) handleContextError(ctx context.Context, runState RunState, toolResults []contracts.ToolResult) *contracts.AgentResponse {',
    $t + 'err := ctx.Err()',
    $t + 'if err == nil {',
    $t + $t + 'return nil',
    $t + '}',
    $t + 'runStatus := RuntimeRunStatusFailed',
    $t + 'statusCode := contracts.ErrorProviderTimeout',
    $t + 'messageText := "request timed out"',
    $t + 'if errors.Is(err, context.Canceled) {',
    $t + $t + 'runStatus = RuntimeRunStatusCancelled',
    $t + $t + 'statusCode = contracts.ErrorInternal',
    $t + $t + 'messageText = "request canceled"',
    $t + '}',
    $t + 'if finishErr := r.finishRunState(ctx, runState, runStatus); finishErr != nil {',
    $t + $t + 'return &contracts.AgentResponse{Error: finishErr, Message: finishErr.Message, Status: contracts.AgentStatusFailed}',
    $t + '}',
    $t + 'return &contracts.AgentResponse{',
    $t + $t + 'Status:      contracts.AgentStatusFailed,',
    $t + $t + 'ToolResults: toolResults,',
    $t + $t + 'Error: &contracts.ErrorShape{',
    $t + $t + $t + 'Code:      statusCode,',
    $t + $t + $t + 'Message:   messageText,',
    $t + $t + $t + 'Source:    contracts.ErrorSourceAgent,',
    $t + $t + $t + 'Retryable: false,',
    $t + $t + '},',
    $t + $t + 'Message: messageText,',
    $t + '}',
    '}'
)
$result = $before + $newFunc + $after
[System.IO.File]::WriteAllLines("D:\Vinsmart\n2task4\V-Claw\internal\agent\runtime.go", $result)

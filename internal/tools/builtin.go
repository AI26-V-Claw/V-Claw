package tools

import (
	"context"
	"fmt"
	"math"
	"time"
)

type CurrentTimeTool struct {
	now func() time.Time
}

func NewCurrentTimeTool() CurrentTimeTool {
	return CurrentTimeTool{now: time.Now}
}

func NewCurrentTimeToolWithClock(now func() time.Time) CurrentTimeTool {
	return CurrentTimeTool{now: now}
}

func (t CurrentTimeTool) Name() string {
	return "get_current_time"
}

func (t CurrentTimeTool) Description() string {
	return "Returns the current local time in ISO-8601 format."
}

func (t CurrentTimeTool) Parameters() ToolSchema {
	return ToolSchema{
		"type":                 "object",
		"properties":           map[string]any{},
		"additionalProperties": false,
	}
}

func (t CurrentTimeTool) Capability() Capability {
	return CapabilityReadOnly
}

func (t CurrentTimeTool) RiskLevel() RiskLevel {
	return RiskLevelSafeRead
}

func (t CurrentTimeTool) Execute(_ context.Context, call ToolCall) ToolResult {
	now := t.now
	if now == nil {
		now = time.Now
	}

	currentTime := now().Format(time.RFC3339)
	return ToolResult{
		ToolCallID:     call.ID,
		ToolName:       call.Name,
		Success:        true,
		ContentForLLM:  "Current time: " + currentTime,
		ContentForUser: currentTime,
	}
}

type CalculatorTool struct{}

func NewCalculatorTool() CalculatorTool {
	return CalculatorTool{}
}

func (CalculatorTool) Name() string {
	return "calculator"
}

func (CalculatorTool) Description() string {
	return "Performs a safe arithmetic operation on two numbers."
}

func (CalculatorTool) Parameters() ToolSchema {
	return ToolSchema{
		"type": "object",
		"properties": map[string]any{
			"operation": map[string]any{
				"type": "string",
				"enum": []string{"add", "subtract", "multiply", "divide"},
			},
			"a": map[string]any{"type": "number"},
			"b": map[string]any{"type": "number"},
		},
		"required":             []string{"operation", "a", "b"},
		"additionalProperties": false,
	}
}

func (CalculatorTool) Capability() Capability {
	return CapabilityReadOnly
}

func (CalculatorTool) RiskLevel() RiskLevel {
	return RiskLevelSafeCompute
}

func (CalculatorTool) Execute(_ context.Context, call ToolCall) ToolResult {
	operation, ok := call.Arguments["operation"].(string)
	if !ok || operation == "" {
		return invalidArgumentResult(call, "operation must be one of: add, subtract, multiply, divide")
	}

	a, ok := numberArgument(call.Arguments, "a")
	if !ok {
		return invalidArgumentResult(call, "a must be a number")
	}

	b, ok := numberArgument(call.Arguments, "b")
	if !ok {
		return invalidArgumentResult(call, "b must be a number")
	}

	var result float64
	switch operation {
	case "add":
		result = a + b
	case "subtract":
		result = a - b
	case "multiply":
		result = a * b
	case "divide":
		if b == 0 {
			return invalidArgumentResult(call, "b must not be zero for divide")
		}
		result = a / b
	default:
		return invalidArgumentResult(call, "unsupported operation: "+operation)
	}

	content := fmt.Sprintf("%s(%s, %s) = %s", operation, formatNumber(a), formatNumber(b), formatNumber(result))
	return ToolResult{
		ToolCallID:     call.ID,
		ToolName:       call.Name,
		Success:        true,
		ContentForLLM:  content,
		ContentForUser: content,
	}
}

func RegisterBuiltInTools(registry *ToolRegistry) error {
	if err := registry.RegisterWithEntry(NewCurrentTimeTool(), ToolRegistryEntry{Group: "builtin"}); err != nil {
		return err
	}
	if err := registry.RegisterWithEntry(NewCalculatorTool(), ToolRegistryEntry{Group: "builtin"}); err != nil {
		return err
	}
	return nil
}

func invalidArgumentResult(call ToolCall, message string) ToolResult {
	return ToolResult{
		ToolCallID:     call.ID,
		ToolName:       call.Name,
		Success:        false,
		ContentForLLM:  "Invalid tool arguments: " + message,
		ContentForUser: "Tham số tool không hợp lệ: " + message,
		Error: &ToolError{
			Code:    ErrorInvalidArgument,
			Message: message,
		},
	}
}

func numberArgument(args map[string]any, name string) (float64, bool) {
	value, ok := args[name]
	if !ok {
		return 0, false
	}

	switch v := value.(type) {
	case int:
		return float64(v), true
	case int8:
		return float64(v), true
	case int16:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	case uint:
		return float64(v), true
	case uint8:
		return float64(v), true
	case uint16:
		return float64(v), true
	case uint32:
		return float64(v), true
	case uint64:
		return float64(v), true
	case float32:
		return float64(v), true
	case float64:
		return v, true
	default:
		return 0, false
	}
}

func formatNumber(value float64) string {
	if math.Trunc(value) == value {
		return fmt.Sprintf("%.0f", value)
	}
	return fmt.Sprintf("%g", value)
}

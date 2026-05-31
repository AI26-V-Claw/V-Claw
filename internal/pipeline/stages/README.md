# Pipeline Stages

## Overview

This module contains pipeline stages for processing intent classification results. Each stage performs a specific validation or transformation step in the intent processing pipeline.

## Components

### Parameter Validator (`param_validator.go`)

Validates parameters for tool calls before execution.

#### Features

1. **Required Parameter Check**: Ensures all required parameters are provided
2. **Type Validation**: Validates parameter types (string, int, bool, path, email)
3. **Security Validation**: Checks for dangerous patterns in paths and commands
4. **Clarification Generation**: Generates user-friendly messages for missing parameters

#### Usage

```go
package main

import (
    "fmt"
    "vclaw/internal/agent"
    "vclaw/internal/pipeline/stages"
)

func main() {
    validator := stages.NewParamValidator()
    
    toolCall := agent.ToolCall{
        Name: "delete_file",
        Category: agent.ToolCategoryDangerousWrite,
        Parameters: map[string]interface{}{
            "path": "/tmp/old.log",
            // Missing "confirm" parameter
        },
    }
    
    validation, err := validator.Validate(toolCall)
    if err != nil {
        panic(err)
    }
    
    if !validation.IsValid {
        fmt.Println("Missing parameters:", validation.Missing)
        
        // Generate clarification request
        message := validator.GenerateClarificationRequest(validation, toolCall.Name)
        fmt.Println(message)
        // Output: "Để thực hiện delete_file, tôi cần thêm thông tin: confirm"
    }
}
```

#### Security Validations

**Path Validation**:
- ❌ Directory traversal: `../../../etc/passwd`
- ❌ Command injection: `/tmp/file | rm -rf /`
- ❌ Command separator: `/tmp/file; rm -rf /`
- ❌ Redirection: `/tmp/file > /dev/null`
- ❌ Command substitution: `/tmp/$(whoami)`

**Email Validation**:
- ✅ Valid: `user@example.com`
- ❌ Missing @: `userexample.com`
- ❌ Missing domain: `user@`
- ❌ Missing username: `@domain.com`

#### Validation Flow

```
ToolCall
    │
    ▼
┌─────────────────┐
│ Get Tool Schema │
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│ Check Required  │
│   Parameters    │
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│ Validate Types  │
│  & Formats      │
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│ Security Check  │
└────────┬────────┘
         │
         ▼
  ParameterValidation
```

## Testing

Run tests:
```bash
go test ./internal/pipeline/stages/... -v
```

Test coverage:
```bash
go test ./internal/pipeline/stages/... -cover
```

## Test Cases

### Valid Parameters
- ✅ All required parameters provided
- ✅ Correct parameter types
- ✅ Safe paths and emails

### Missing Parameters
- ✅ Detect missing required parameters
- ✅ Generate clarification messages
- ✅ List all missing parameters

### Invalid Parameters
- ✅ Reject dangerous path patterns
- ✅ Reject invalid email formats
- ✅ Reject wrong parameter types

### Multiple Tool Calls
- ✅ Validate all tool calls in sequence
- ✅ Generate combined clarification messages

## Integration

This module integrates with:
- `internal/agent/` - Uses tool definitions and types
- `internal/agent/intent_classifier.go` - Validates extracted tool calls
- Future: `internal/pipeline/stages/workflow_splitter.go` - Validates composite workflows

## Next Steps

### Phase 3: Workflow Splitter
- [ ] Implement `workflow_splitter.go`
- [ ] Split composite actions into multi-step workflows
- [ ] Validate each step independently
- [ ] Handle step dependencies

### Phase 4: Enhanced Validation
- [ ] Add custom validators per tool
- [ ] Implement parameter transformation
- [ ] Add validation caching
- [ ] Support conditional parameters

## References

- [Intent Classification Spec](../../../intent_classification_spec.md)
- [Agent Module](../../agent/README.md)
- [Tool Registry](../../agent/tool_registry.go)

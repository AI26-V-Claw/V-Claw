# Agent Intent Classification Module

## Overview

This module implements the Intent Classification system as specified in `intent_classification_spec.md`. It provides accurate intent classification (>80% accuracy target) with safety guardrails to prevent AI from taking dangerous actions without proper confirmation.

## Components

### 1. Core Types (`types.go`)
Defines all data structures:
- `IntentType`: GREETING, READ_INFO, DANGEROUS_ACTION, COMPOSITE_ACTION
- `Intent`: Classification result with confidence, parameters, and tool calls
- `ToolCall`: Tool invocation with parameters
- `ToolDefinition`: Tool schema and metadata
- `ParameterValidation`: Parameter validation result

### 2. Configuration (`config.go`)
Confidence thresholds for different intent types:
- GREETING: 0.0 (always accept)
- READ_INFO: 0.70
- DANGEROUS_ACTION: 0.90
- COMPOSITE_ACTION: 0.85

### 3. Tool Registry (`tool_registry.go`)
Centralized registry of all available tools:
- **Safe Read Tools**: `gmail.listEmails`, `gmail.listLabels`, `gmail.getProfile`, `gmail.listThreads`, `gmail.getThread`, `gmail.listDrafts`, `gmail.getDraft`, `calendar.listEvents`, `chat.listSpaces`, `chat.listMembers`, `chat.findSpacesByMembers`, `chat.listMessages`, `people.searchDirectory`
- **Sensitive Read Tools**: `gmail.getEmail`
- **Dangerous Write Tools**: `gmail.createDraft`, `gmail.updateDraft`, `gmail.sendDraft`, `gmail.deleteDraft`, `gmail.replyDraft`, `gmail.forwardDraft`, `gmail.downloadAttachments`, `gmail.modifyMessage`, `gmail.batchModifyMessages`, `gmail.trashMessage`, `gmail.untrashMessage`, `calendar.createEvent`, `calendar.updateEvent`, `calendar.deleteEvent`, `chat.sendMessage`, `chat.updateMessage`, `chat.deleteMessage`, `chat.createSpace`, `chat.addMember`, `chat.removeMember`
- **Built-in Tools**: `calculator`, `get_current_time`
- **Execution Tools**: `sandbox.runPython`, `sandbox.runShell`
- **Communication Tools**: `gmail.createDraft`, `gmail.updateDraft`, `gmail.sendDraft`, `gmail.replyDraft`, `gmail.forwardDraft`, `chat.sendMessage`, `chat.updateMessage`, `chat.createSpace`, `chat.addMember`

Each tool has:
- Category (SAFE_READ, SENSITIVE_READ, DANGEROUS_WRITE, EXECUTION, COMMUNICATION)
- Required parameters
- Timeout configuration
- Danger flag and confirmation requirement

### 4. Confidence Scorer (`confidence.go`)
Calculates confidence scores for intent classification:
- `CalculateFromLogprobs()`: Uses LLM API logprobs (when available)
- `CalculateHeuristic()`: Fallback heuristic-based scoring
- `ShouldAskForClarification()`: Determines if clarification is needed

### 5. Intent Classifier (`intent_classifier.go`)
Main classification logic:
- `Classify()`: Classifies user input into intent type
- Extracts tool calls based on intent
- Validates parameters
- Generates clarification requests when needed

## Usage

```go
package main

import (
    "context"
    "fmt"
    "vclaw/internal/agent/intent"
)

func main() {
    // Create classifier with default config
    classifier := intent.NewClassifier(intent.DefaultConfig)
    
    // Classify user input
    result, err := intent.Classify(context.Background(), classifier, "Xóa file config.json")
    if err != nil {
        panic(err)
    }
    
    // Check if clarification is needed
    if result.NeedsClarification {
        fmt.Println("Question:", result.ClarificationMessage)
        return
    }
    
    // Check intent type
    classified := result.Intent
    fmt.Printf("Intent: %s (confidence: %.2f)\n", classified.Type, classified.Confidence)
    
    // Check if confirmation is needed
    if classified.NeedsConfirm {
        fmt.Println("⚠️  This action requires confirmation!")
    }
    
    // Check for missing parameters
    if len(classified.MissingParams) > 0 {
        fmt.Printf("Missing parameters: %v\n", classified.MissingParams)
        return
    }
    
    // Execute tool calls
    for _, toolCall := range classified.ToolCalls {
        fmt.Printf("Tool: %s, Params: %v\n", toolCall.Name, toolCall.Parameters)
    }
}
```

## Safety Rules

### 1. Missing Parameters Rule
**CRITICAL**: AI MUST NOT guess or hallucinate missing parameters for dangerous actions.

```go
// ❌ WRONG: AI guesses the file path
User: "Xóa file config đi"
AI: *assumes /etc/config.json and deletes it*

// ✅ CORRECT: AI asks for clarification
User: "Xóa file config đi"
AI: "Bạn muốn xóa file config nào? Vui lòng cung cấp đường dẫn chính xác."
```

### 2. Memory Isolation Rule
**CRITICAL**: AI MUST NOT use information from old sessions for dangerous actions.

```go
// ❌ WRONG: AI uses info from yesterday
Session 1 (yesterday): "File config ở /etc/app.conf"
Session 2 (today): "Xóa file config"
AI: *uses /etc/app.conf from yesterday*

// ✅ CORRECT: AI asks again
Session 2 (today): "Xóa file config"
AI: "Bạn muốn xóa file config nào? Vui lòng cung cấp đường dẫn."
```

### 3. Confirmation Rule
All DANGEROUS_ACTION intents require explicit user confirmation:
- Delete operations
- File modifications
- Email sending
- Command execution

### 4. Confidence Threshold Rule
- DANGEROUS_ACTION requires confidence >= 0.90
- If confidence < threshold, ask for clarification
- If confidence in ambiguous range (0.60-0.85), show multiple choice

## Testing

Run unit tests:
```bash
go test ./internal/agent/... -v
```

Run specific test:
```bash
go test ./internal/agent/intent -run TestClassify_Greeting -v
```

Run with coverage:
```bash
go test ./internal/agent/... -cover
```

## Test Cases

### Basic Classification (TC001-TC003)
- ✅ TC001: Greeting intent
- ✅ TC002: Read info intent
- ✅ TC003: Dangerous action intent

### Missing Parameters (TC004-TC005)
- ✅ TC004: Delete without path
- ✅ TC005: Send email without details

### Composite Actions (TC008-TC009)
- ✅ TC008: Find and delete
- ✅ TC009: Read and send

### Ambiguous Input (TC010-TC011)
- ✅ TC010: Ambiguous action
- ✅ TC011: Very vague input

## Performance Targets

| Metric | Target | Status |
|--------|--------|--------|
| Overall Accuracy | > 80% | 🔴 TBD |
| GREETING Precision | > 75% | 🔴 TBD |
| READ_INFO Precision | > 75% | 🔴 TBD |
| DANGEROUS_ACTION Precision | > 75% | 🔴 TBD |
| False Positive Rate (DANGEROUS) | < 5% | 🔴 TBD |
| Classification Latency (p95) | < 500ms | 🔴 TBD |

## Next Steps

### Phase 2: Safety Guardrails (Week 3)
- [ ] Implement `input_guard.go` - Prompt injection detection
- [ ] Implement memory isolation rules
- [ ] Add security tests

### Phase 3: Advanced Features (Week 4)
- [ ] Implement `workflow_splitter.go` - Composite actions
- [ ] Add confidence-based multiple choice UI
- [ ] Implement retry & timeout logic

### Phase 4: Audit & Rollback (Week 5)
- [ ] Implement audit logging
- [ ] Implement rollback mechanism
- [ ] Add audit log storage

## Integration

This module integrates with:
- `internal/agent/intent/` - Intent classification and parameter validation
- `internal/audit/` - Action logging (Phase 4)
- `internal/memory/` - Session and long-term memory (Phase 2-3)
- `internal/safety/` - Risk classification (Phase 2)

## References

- [Intent Classification Spec](../../intent_classification_spec.md)
- [System Design](../../docs/01-system-design.md)
- [Active Modules](../../ACTIVE_MODULES.md)

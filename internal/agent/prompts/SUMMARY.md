# Intent Classifier Prompt - Quick Summary

## What is this?

A comprehensive system prompt for classifying user intents with >80% accuracy while maintaining strict safety guardrails.

## Files Created

```
internal/agent/prompts/
├── intent_classifier_prompt.md  # Main prompt template (4,500 tokens)
├── prompts.go                   # Go utilities for building prompts
├── prompts_test.go              # Unit tests
├── README.md                    # Detailed documentation
└── SUMMARY.md                   # This file

examples/intent_classification/
└── prompt_usage.go              # Usage examples

docs/
└── intent-classifier-prompt-design.md  # Design document
```

## Quick Start

```go
import "github.com/yourusername/goclaw/internal/agent/prompts"

// Basic usage
builder := prompts.NewIntentClassifierPrompt()
fullPrompt := builder.BuildWithUserInput("Xóa file config.json")

// Send to LLM
response := callLLM(fullPrompt)

// Validate and parse
if err := prompts.ValidateJSONResponse(response); err != nil {
    // Handle error
}

var intent agent.Intent
json.Unmarshal([]byte(response), &intent)
```

## Key Features

### 1. Five Intent Types
- **GREETING**: Social interactions (no tools)
- **READ_INFO**: Information retrieval (safe tools)
- **DANGEROUS_ACTION**: System modifications (requires confirmation)
- **COMPOSITE_ACTION**: Multi-step workflows
- **UNKNOWN**: Ambiguous requests (needs clarification)

### 2. Four Safety Rules
1. **No Hallucination**: Never infer missing parameters for dangerous actions
2. **Memory Isolation**: Treat each request as standalone
3. **Explicit Confirmation**: Always confirm dangerous operations
4. **Composite Detection**: Automatically split multi-step workflows

### 3. Confidence Scoring
```
confidence = clarity (0.3) + completeness (0.4) + consistency (0.3)

Thresholds:
- GREETING: 0.0 (always accept)
- READ_INFO: 0.70
- DANGEROUS_ACTION: 0.90
- COMPOSITE_ACTION: 0.85
```

### 4. Structured Output
```json
{
  "intent_type": "DANGEROUS_ACTION",
  "confidence": 0.95,
  "required_params": ["path", "confirm"],
  "provided_params": {"path": "/tmp/test.txt"},
  "missing_params": ["confirm"],
  "tool_calls": [...],
  "needs_confirm": true,
  "reasoning": "Explicit confirmation required for file deletion"
}
```

## Example Scenarios

### Scenario 1: Safe Read (Execute Immediately)
```
Input: "Đọc file /etc/config.json"
Output: intent_type="READ_INFO", confidence=0.95, needs_confirm=false
Action: Execute read_file tool
```

### Scenario 2: Dangerous with Missing Params (Ask for Clarification)
```
Input: "Xóa file config"
Output: intent_type="DANGEROUS_ACTION", missing_params=["path"], needs_confirm=true
Action: Ask "Bạn muốn xóa file config nào? Vui lòng cung cấp đường dẫn."
```

### Scenario 3: Composite Action (Multi-step Workflow)
```
Input: "Tìm và xóa file log cũ"
Output: intent_type="COMPOSITE_ACTION", tool_calls=[find_files, delete_files]
Action: 
  1. Execute find_files
  2. Show results: "Tìm thấy 15 file"
  3. Ask confirmation: "Xóa 15 file này?"
  4. If confirmed → Execute delete_files
```

### Scenario 4: Ambiguous (Multiple Choice)
```
Input: "Xử lý file config"
Output: intent_type="UNKNOWN", confidence=0.65, needs_confirm=true
Action: Ask "Bạn muốn: A) Đọc file, B) Sửa file, C) Xóa file?"
```

## Advanced Usage

### With Context
```go
tools := map[string]interface{}{
    "read_file": toolDef,
    "delete_file": toolDef,
}

history := []string{
    "User: Tìm file config",
    "AI: Tìm thấy /etc/config.json",
}

prompt := prompts.NewIntentClassifierPrompt().
    WithToolRegistry(tools).
    WithUserContext("user123", "/home/user").
    WithSessionHistory(history, 5).
    BuildWithUserInput("Xóa file đó")
```

### Validation
```go
// Validate JSON format
if err := prompts.ValidateJSONResponse(response); err != nil {
    return fmt.Errorf("invalid format: %w", err)
}

// Parse into struct
var intent agent.Intent
if err := json.Unmarshal([]byte(response), &intent); err != nil {
    return fmt.Errorf("parse error: %w", err)
}

// Check if clarification needed
if intent.NeedsConfirm || len(intent.MissingParams) > 0 {
    return askUserForClarification(intent)
}
```

## Performance

| Metric | Value |
|--------|-------|
| Prompt size | ~4,500 tokens (base) |
| With context | ~6,500 tokens |
| Latency | 1-2 seconds |
| Cost (Gemini Flash) | ~$0.0005 per call |
| Target accuracy | > 80% |

## Testing

```bash
# Run unit tests
go test -v ./internal/agent/prompts

# Run with coverage
go test -cover ./internal/agent/prompts

# Run benchmarks
go test -bench=. ./internal/agent/prompts

# Run integration tests (requires LLM API)
go test -v ./internal/agent -tags=integration
```

## Common Issues

### Issue: LLM returns markdown instead of JSON
**Solution**: 
- Set `temperature=0` for deterministic output
- Use `ResponseMIMEType: "application/json"` if supported
- Post-process to extract JSON from markdown

### Issue: Low confidence scores
**Solution**:
- Add more context (tools, working directory)
- Include recent conversation history
- Check if user input is too vague

### Issue: False positives for DANGEROUS_ACTION
**Solution**:
- Increase `DangerousActionMinConfidence` threshold
- Add more examples of safe operations
- Review tool categorization

## Next Steps

1. ✅ System prompt created
2. ⏳ Implement `intent_classifier.go` (uses this prompt)
3. ⏳ Create evaluation dataset (500+ samples)
4. ⏳ Run accuracy tests
5. ⏳ Deploy to staging
6. ⏳ Monitor production metrics

## Resources

- **Detailed Docs**: [README.md](README.md)
- **Design Doc**: [../../../docs/intent-classifier-prompt-design.md](../../../docs/intent-classifier-prompt-design.md)
- **Examples**: [../../../examples/intent_classification/prompt_usage.go](../../../examples/intent_classification/prompt_usage.go)
- **Spec**: [../../../intent_classification_spec.md](../../../intent_classification_spec.md)

## Questions?

Contact: V-Claw Team  
Last Updated: 2026-05-31

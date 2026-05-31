# Intent Classifier Prompts

This package contains system prompts for the Intent Classification system.

## Overview

The Intent Classifier uses a carefully crafted system prompt to guide the LLM in classifying user intents with high accuracy (>80%) while maintaining strict safety guardrails.

## Files

- **`intent_classifier_prompt.md`**: The main system prompt template (embedded at compile time)
- **`prompts.go`**: Go utilities for building and managing prompts

## Usage

### Basic Usage

```go
package main

import (
    "fmt"
    "github.com/yourusername/goclaw/internal/agent/prompts"
)

func main() {
    // Create a new prompt builder
    builder := prompts.NewIntentClassifierPrompt()
    
    // Build prompt with user input
    userInput := "Xóa file config.json"
    fullPrompt := builder.BuildWithUserInput(userInput)
    
    // Send to LLM
    response := callLLM(fullPrompt)
    
    // Validate response
    if err := prompts.ValidateJSONResponse(response); err != nil {
        fmt.Printf("Invalid response: %v\n", err)
        return
    }
    
    // Parse JSON response
    // ... (see intent_classifier.go for full implementation)
}
```

### Advanced Usage with Context

```go
// Add tool registry
tools := map[string]interface{}{
    "read_file": toolDefinitions["read_file"],
    "delete_file": toolDefinitions["delete_file"],
}

// Add user context
builder := prompts.NewIntentClassifierPrompt().
    WithToolRegistry(tools).
    WithUserContext("user123", "/home/user/project").
    WithSessionHistory([]string{
        "User: Tìm file config",
        "AI: Tìm thấy /etc/config.json",
    }, 5)

// Build final prompt
fullPrompt := builder.BuildWithUserInput("Xóa file đó")

// The prompt will include:
// 1. Base intent classification instructions
// 2. Available tools in this session
// 3. User context (working directory, etc.)
// 4. Recent conversation history (with warning not to use for dangerous actions)
// 5. User input to classify
```

## Prompt Structure

The system prompt is organized into the following sections:

### 1. Role & Objective
Defines the AI's role as an Intent Classification Specialist.

### 2. Intent Types
Detailed descriptions of 5 intent types:
- `GREETING`: Social interactions
- `READ_INFO`: Information retrieval
- `DANGEROUS_ACTION`: System modifications
- `COMPOSITE_ACTION`: Multi-step workflows
- `UNKNOWN`: Ambiguous requests

### 3. Output Format
Specifies the exact JSON schema for responses.

### 4. Classification Rules
Provides algorithms for:
- Confidence scoring
- Parameter extraction
- Ambiguity handling
- Safety checks

### 5. Critical Safety Rules
Four non-negotiable rules:
1. No hallucination for dangerous actions
2. Memory isolation
3. Explicit confirmation
4. Composite action detection

### 6. Tool Registry Reference
Lists all available tools with their parameters.

### 7. Example Classifications
7 detailed examples covering common scenarios.

### 8. Edge Cases
Handles special situations like:
- Prompt injection attempts
- Vague references
- Mixed language input

## Design Principles

### 1. Safety First
- Dangerous actions require confidence > 0.90
- Missing parameters trigger clarification requests
- No inference from previous context for dangerous operations

### 2. High Accuracy
- Clear classification criteria
- Confidence scoring algorithm
- Ambiguity detection and handling

### 3. Memory Isolation
- Each request treated as standalone
- Previous context only for reference, not execution
- Explicit warnings against using old information

### 4. Structured Output
- JSON-only responses
- Strict schema validation
- No markdown or explanations outside JSON

## Confidence Scoring Algorithm

The prompt instructs the LLM to calculate confidence using three factors:

```
confidence = clarity_score (0.3) + completeness_score (0.4) + consistency_score (0.3)

Where:
- clarity_score: How clear and specific is the request?
- completeness_score: Are all required parameters provided?
- consistency_score: Does it match known patterns?
```

### Confidence Thresholds

| Intent Type | Min Confidence | Action |
|-------------|---------------|--------|
| GREETING | 0.0 | Always accept |
| READ_INFO | 0.70 | Execute if params complete |
| DANGEROUS_ACTION | 0.90 | Require confirmation |
| COMPOSITE_ACTION | 0.85 | Split workflow, confirm dangerous steps |
| UNKNOWN | < 0.60 | Ask for clarification |

### Ambiguous Range (0.60 - 0.85)
When confidence falls in this range:
1. Set `needs_confirm = true`
2. Provide reasoning explaining ambiguity
3. Suggest clarification questions or multiple choice options

## Safety Guardrails

### Rule #1: No Hallucination
```
❌ FORBIDDEN:
User: "Xóa file config"
AI: Assumes path from previous conversation

✅ REQUIRED:
AI: "Bạn muốn xóa file config nào? Vui lòng cung cấp đường dẫn."
```

### Rule #2: Memory Isolation
```
❌ FORBIDDEN:
Day 1: User mentions "/etc/app.conf"
Day 3: User says "Xóa file config"
AI: Uses /etc/app.conf from Day 1

✅ REQUIRED:
AI: Treats Day 3 request as standalone, asks for path
```

### Rule #3: Explicit Confirmation
For DANGEROUS_ACTION, `needs_confirm = true` if:
- ANY required parameter is missing
- Confidence < 0.90
- User uses vague references ("it", "that", "the file")

### Rule #4: Composite Action Detection
Multi-step workflows are automatically detected and split:
```
User: "Tìm và xóa file log cũ"

AI Response:
{
  "intent_type": "COMPOSITE_ACTION",
  "tool_calls": [
    {"name": "find_files", "category": "SAFE_READ", ...},
    {"name": "delete_files", "category": "DANGEROUS_WRITE", ...}
  ],
  "needs_confirm": true
}
```

## Testing the Prompt

### Unit Tests
See `prompts_test.go` for unit tests covering:
- Prompt building
- Context injection
- JSON validation

### Integration Tests
See `../intent_classifier_test.go` for integration tests with real LLM calls.

### Evaluation Dataset
See `../../evaluation/test_cases.json` for 500+ test cases covering:
- All intent types
- Edge cases
- Ambiguous inputs
- Multi-language inputs

## Customization

### Adding New Intent Types
1. Update `intent_classifier_prompt.md`:
   - Add new intent type to "Intent Types" section
   - Add examples
   - Update classification rules

2. Update `../types.go`:
   - Add new `IntentType` constant
   - Update `Intent` struct if needed

3. Update `../config.go`:
   - Add confidence threshold for new type

### Adding New Tools
1. Update "Tool Registry Reference" section in prompt
2. Add tool definition to `../tool_registry.go`
3. Update examples if needed

### Adjusting Confidence Thresholds
Edit `../config.go`:
```go
var DefaultConfidenceConfig = ConfidenceConfig{
    ReadInfoMinConfidence:        0.70, // Adjust this
    DangerousActionMinConfidence: 0.90, // Adjust this
    // ...
}
```

## Performance Considerations

### Token Usage
- Base prompt: ~4,500 tokens
- With context: ~5,500 tokens
- With history (5 turns): ~6,500 tokens

**Recommendation**: Use Gemini 1.5 Flash or GPT-4o-mini for cost efficiency.

### Latency
- Average classification time: 1-2 seconds
- With tool registry: +0.2 seconds
- With session history: +0.3 seconds

**Optimization**: Cache the base prompt, only rebuild when context changes.

### Cost Estimation
Using Gemini 1.5 Flash (as of 2026):
- Input: $0.075 per 1M tokens
- Output: $0.30 per 1M tokens
- Average cost per classification: ~$0.0005

## Troubleshooting

### Issue: LLM returns markdown instead of JSON
**Solution**: Check if you're using the correct model. Some models ignore "no markdown" instructions. Try:
- Adding temperature=0 for more deterministic output
- Post-processing to extract JSON from markdown code blocks

### Issue: Low confidence scores
**Solution**: 
- Check if user input is too vague
- Add more context (working directory, available tools)
- Review recent conversation history

### Issue: False positives for DANGEROUS_ACTION
**Solution**:
- Increase `DangerousActionMinConfidence` threshold
- Add more examples of safe operations to prompt
- Review tool categorization in registry

### Issue: Missing parameters not detected
**Solution**:
- Verify tool definitions in `tool_registry.go`
- Check parameter extraction logic in prompt
- Add more examples of incomplete requests

## References

- [Intent Classification Spec](../../../intent_classification_spec.md)
- [Implementation Plan](../../../implementation_plan.md)
- [System Design](../../../docs/01-system-design.md)

## Contributing

When modifying the prompt:
1. Update `intent_classifier_prompt.md`
2. Add test cases to `../../evaluation/test_cases.json`
3. Run evaluation: `go test -v ./internal/evaluation`
4. Ensure accuracy > 80% on test dataset
5. Update this README if adding new features

## License

Internal use only - Part of V-Claw project.

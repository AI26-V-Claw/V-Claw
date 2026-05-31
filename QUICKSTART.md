# Intent Classification System - Quick Start Guide

**Last Updated**: 2026-05-31

## 📋 Prerequisites

### Required
- **Go 1.26+**: Download from [golang.org](https://golang.org/dl/)
- **Git**: For version control

### Optional
- **VS Code** with Go extension
- **GoLand** or other Go IDE

## 🚀 Installation

### 1. Verify Go Installation

```bash
go version
# Should output: go version go1.26 or higher
```

If Go is not installed:
- **Windows**: Download installer from golang.org
- **macOS**: `brew install go`
- **Linux**: `sudo apt install golang-go` or download from golang.org

### 2. Clone Repository

```bash
cd /path/to/V-Claw
```

### 3. Verify Module

```bash
go mod verify
go mod tidy
```

## 🧪 Running Tests

### Run All Tests

```bash
# Test agent module
go test ./internal/agent/... -v

# Test pipeline stages
go test ./internal/pipeline/stages/... -v

# Test everything
go test ./... -v
```

### Run Specific Test

```bash
# Test greeting classification
go test ./internal/agent -run TestIntentClassifier_Classify_Greeting -v

# Test parameter validation
go test ./internal/pipeline/stages -run TestParamValidator_Validate -v
```

### Run with Coverage

```bash
# Generate coverage report
go test ./internal/agent/... -cover
go test ./internal/pipeline/stages/... -cover

# Generate detailed coverage HTML
go test ./internal/agent/... -coverprofile=coverage.out
go tool cover -html=coverage.out
```

## 🎮 Running Demo

### Basic Usage

```bash
# Greeting intent
go run examples/intent_classification/main.go "Chào buổi sáng"

# Read info intent
go run examples/intent_classification/main.go "Đọc file config.json"

# Dangerous action intent
go run examples/intent_classification/main.go "Xóa file config.json"

# Composite action intent
go run examples/intent_classification/main.go "Tìm và xóa các file log cũ"

# Ambiguous intent
go run examples/intent_classification/main.go "Xử lý file config"
```

### Expected Output

#### Example 1: Greeting
```bash
$ go run examples/intent_classification/main.go "Chào buổi sáng"

📝 User Input: "Chào buổi sáng"

🎯 Intent Type: GREETING
📊 Confidence: 95.00%
💭 Reasoning: Classified as GREETING with confidence 0.95 based on input: "Chào buổi sáng"

ℹ️  No tool calls required (direct response)

✅ Safe to execute without confirmation
```

#### Example 2: Dangerous Action
```bash
$ go run examples/intent_classification/main.go "Xóa file config.json"

📝 User Input: "Xóa file config.json"

🎯 Intent Type: DANGEROUS_ACTION
📊 Confidence: 90.00%
💭 Reasoning: Classified as DANGEROUS_ACTION with confidence 0.90 based on input: "Xóa file config.json"

🔧 Tool Calls:
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

1. delete_file (DANGEROUS_WRITE)
   Timeout: 60s
   Parameters:
     - path: config.json
   ⚠️  Missing Parameters: [confirm]

   💬 Để thực hiện delete_file, tôi cần thêm thông tin: confirm

⚠️  CONFIRMATION REQUIRED
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
This action is dangerous and requires explicit user confirmation.
User must approve before execution.

📋 Missing Parameters Summary:
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  - confirm

⚠️  Cannot execute until all parameters are provided.
```

#### Example 3: Ambiguous Intent
```bash
$ go run examples/intent_classification/main.go "Xử lý file config"

📝 User Input: "Xử lý file config"

🎯 Intent Type: DANGEROUS_ACTION
📊 Confidence: 75.00%
💭 Reasoning: Classified as DANGEROUS_ACTION with confidence 0.75 based on input: "Xử lý file config"

❓ Clarification Needed
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Tôi chưa hiểu rõ ý bạn với câu: "Xử lý file config"

Bạn muốn làm gì?

A) Đọc và hiển thị thông tin (READ_INFO) (confidence: 70.00%)
B) Thực hiện thay đổi/xóa/gửi (DANGEROUS_ACTION) (confidence: 60.00%)
C) Hành động phức hợp nhiều bước (COMPOSITE_ACTION) (confidence: 50.00%)
```

## 🔧 Integration with Main Application

### Step 1: Import the Module

```go
import (
    "context"
    "vclaw/internal/agent"
    "vclaw/internal/pipeline/stages"
)
```

### Step 2: Create Classifier

```go
// Use default configuration
classifier := agent.NewIntentClassifier(agent.DefaultConfidenceConfig)

// Or use custom configuration
customConfig := agent.ConfidenceConfig{
    GreetingMinConfidence:        0.0,
    ReadInfoMinConfidence:        0.75,  // Higher threshold
    DangerousActionMinConfidence: 0.95,  // Even higher for dangerous
    CompositeActionMinConfidence: 0.85,
    AmbiguousRangeLow:            0.60,
    AmbiguousRangeHigh:           0.85,
}
classifier := agent.NewIntentClassifier(customConfig)
```

### Step 3: Classify User Input

```go
result, err := classifier.Classify(context.Background(), userInput)
if err != nil {
    // Handle error
    return err
}

if result.Error != nil {
    // Handle classification error
    return result.Error
}
```

### Step 4: Handle Result

```go
intent := result.Intent

// Check if clarification is needed
if result.NeedsClarification {
    // Show clarification options to user
    showClarificationUI(result.ClarificationOptions)
    return
}

// Check for missing parameters
if len(intent.MissingParams) > 0 {
    // Ask user for missing parameters
    askForParameters(intent.MissingParams)
    return
}

// Check if confirmation is needed
if intent.NeedsConfirm {
    // Show confirmation dialog
    confirmed := showConfirmationDialog(intent)
    if !confirmed {
        return
    }
}

// Execute tool calls
for _, toolCall := range intent.ToolCalls {
    // Validate parameters
    validator := stages.NewParamValidator()
    validation, err := validator.Validate(toolCall)
    if err != nil {
        return err
    }
    
    if !validation.IsValid {
        // This shouldn't happen if we checked MissingParams above
        return fmt.Errorf("invalid parameters: %v", validation.Missing)
    }
    
    // Execute the tool
    result, err := executeTool(toolCall)
    if err != nil {
        return err
    }
    
    // Log the execution (Phase 4)
    // auditLogger.Log(toolCall, result)
}
```

## 📚 API Reference

### IntentClassifier

```go
type IntentClassifier struct {
    config           ConfidenceConfig
    confidenceScorer *ConfidenceScorer
}

func NewIntentClassifier(config ConfidenceConfig) *IntentClassifier

func (ic *IntentClassifier) Classify(ctx context.Context, userInput string) (*ClassificationResult, error)
```

### ParamValidator

```go
type ParamValidator struct{}

func NewParamValidator() *ParamValidator

func (pv *ParamValidator) Validate(toolCall agent.ToolCall) (*agent.ParameterValidation, error)

func (pv *ParamValidator) ValidateAll(toolCalls []agent.ToolCall) ([]*agent.ParameterValidation, error)

func (pv *ParamValidator) GenerateClarificationRequest(validation *agent.ParameterValidation, toolName string) string
```

### Key Types

```go
type IntentType string
const (
    IntentGreeting        IntentType = "GREETING"
    IntentReadInfo        IntentType = "READ_INFO"
    IntentDangerousAction IntentType = "DANGEROUS_ACTION"
    IntentComposite       IntentType = "COMPOSITE_ACTION"
    IntentUnknown         IntentType = "UNKNOWN"
)

type Intent struct {
    Type           IntentType
    Confidence     float64
    RequiredParams []string
    ProvidedParams map[string]interface{}
    MissingParams  []string
    ToolCalls      []ToolCall
    NeedsConfirm   bool
    Reasoning      string
    Timestamp      time.Time
}

type ClassificationResult struct {
    Intent               *Intent
    NeedsClarification   bool
    ClarificationOptions *ClarificationOptions
    Error                error
}
```

## 🐛 Troubleshooting

### Issue: "go: command not found"
**Solution**: Install Go from golang.org and add to PATH

### Issue: "package vclaw/internal/agent is not in GOROOT"
**Solution**: Run `go mod tidy` to download dependencies

### Issue: Tests fail with import errors
**Solution**: Ensure you're in the project root directory and run `go mod verify`

### Issue: Demo program doesn't run
**Solution**: 
```bash
# Make sure you're in the project root
cd /path/to/V-Claw

# Run with full path
go run ./examples/intent_classification/main.go "test input"
```

## 📖 Documentation

- [Intent Classification Spec](./intent_classification_spec.md) - Full specification
- [Implementation Status](./IMPLEMENTATION_STATUS.md) - Progress tracking
- [Phase 1 Summary](./PHASE_1_SUMMARY.md) - What was built
- [Phase 1 Checklist](./PHASE_1_CHECKLIST.md) - Detailed checklist
- [Agent Module README](./internal/agent/README.md) - Agent module docs
- [Pipeline Stages README](./internal/pipeline/stages/README.md) - Pipeline docs

## 🤝 Contributing

### Adding a New Tool

1. Add tool definition to `internal/agent/tool_registry.go`:

```go
"my_new_tool": {
    Name:            "my_new_tool",
    Category:        ToolCategorySafeRead,
    Description:     "Description of what it does",
    Dangerous:       false,
    RequiresConfirm: false,
    Timeout:         30,
    Parameters: []ParameterDef{
        {Name: "param1", Type: "string", Required: true},
    },
},
```

2. Add extraction logic in `internal/agent/intent_classifier.go`:

```go
func (ic *IntentClassifier) extractToolCalls(input string, intentType IntentType) []ToolCall {
    // ... existing code ...
    
    if strings.Contains(input, "my keyword") {
        toolCalls = append(toolCalls, ToolCall{
            Name:       "my_new_tool",
            Category:   ToolCategorySafeRead,
            Parameters: ic.extractMyParams(input),
            Timeout:    30,
        })
    }
}
```

3. Add tests in `internal/agent/intent_classifier_test.go`

### Running Tests Before Commit

```bash
# Run all tests
go test ./... -v

# Run with race detector
go test ./... -race

# Run with coverage
go test ./... -cover
```

## 🎯 Next Steps

After completing Phase 1, proceed to:

1. **Phase 2: Safety Guardrails** (Week 3)
   - Implement prompt injection detection
   - Add memory isolation
   - Write security tests

2. **Phase 3: Advanced Features** (Week 4)
   - Implement workflow splitter
   - Add tool executor with retry
   - Enhance composite action handling

3. **Phase 4: Audit & Rollback** (Week 5)
   - Implement audit logging
   - Add rollback mechanism
   - Create audit query interface

See [IMPLEMENTATION_STATUS.md](./IMPLEMENTATION_STATUS.md) for detailed roadmap.

## 📞 Support

For questions or issues:
1. Check documentation in `internal/agent/README.md`
2. Review test cases for usage examples
3. Check [IMPLEMENTATION_STATUS.md](./IMPLEMENTATION_STATUS.md) for known limitations

---

**Happy Coding! 🚀**

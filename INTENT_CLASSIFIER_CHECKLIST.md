# Intent Classifier Implementation Checklist

**Project**: V-Claw Intent Classification System  
**Goal**: Achieve >80% classification accuracy with strict safety guardrails  
**Status**: Phase 1 - System Prompt Completed ✅

---

## Phase 1: System Prompt Design ✅ COMPLETED

### Deliverables
- [x] Main prompt template (`internal/agent/prompts/intent_classifier_prompt.md`)
- [x] Go utilities (`internal/agent/prompts/prompts.go`)
- [x] Unit tests (`internal/agent/prompts/prompts_test.go`)
- [x] Documentation (`internal/agent/prompts/README.md`)
- [x] Design document (`docs/intent-classifier-prompt-design.md`)
- [x] Usage examples (`examples/intent_classification/prompt_usage.go`)
- [x] Quick summary (`internal/agent/prompts/SUMMARY.md`)

### Key Features Implemented
- [x] 5 intent types (GREETING, READ_INFO, DANGEROUS_ACTION, COMPOSITE_ACTION, UNKNOWN)
- [x] 4 critical safety rules (no hallucination, memory isolation, explicit confirmation, composite detection)
- [x] Confidence scoring algorithm (clarity + completeness + consistency)
- [x] Structured JSON output format
- [x] Tool registry integration
- [x] Session history support
- [x] 7 detailed examples
- [x] Edge case handling (prompt injection, vague references, mixed language)

### Testing
- [x] Unit tests for prompt builder
- [x] Unit tests for JSON validation
- [x] Benchmark tests for performance

---

## Phase 2: Core Implementation ⏳ IN PROGRESS

### 2.1 Intent Classifier Module
- [ ] Create `internal/pipeline/stages/intent_classifier.go`
  - [ ] LLM API integration (Gemini/GPT)
  - [ ] Prompt building with context
  - [ ] Response parsing and validation
  - [ ] Error handling and retries
  - [ ] Confidence score extraction
  - [ ] Intent type routing logic

- [ ] Create `internal/pipeline/stages/intent_classifier_test.go`
  - [ ] Unit tests for classification logic
  - [ ] Mock LLM responses
  - [ ] Test all intent types
  - [ ] Test edge cases

### 2.2 Parameter Validator Module
- [ ] Create `internal/pipeline/stages/param_validator.go`
  - [ ] Parameter extraction from user input
  - [ ] Required vs provided comparison
  - [ ] Missing parameter detection
  - [ ] Clarification request generation

- [ ] Create `internal/pipeline/stages/param_validator_test.go`
  - [ ] Test complete parameters (valid)
  - [ ] Test missing parameters (invalid)
  - [ ] Test partial parameters
  - [ ] Test clarification messages

### 2.3 Tool Registry Enhancement
- [ ] Update `internal/agent/tool_registry.go`
  - [ ] Add all standard tools (read_file, delete_file, exec, etc.)
  - [ ] Define parameter schemas
  - [ ] Add parameter validators
  - [ ] Categorize tools (SAFE_READ, DANGEROUS_WRITE, etc.)

### 2.4 Configuration
- [ ] Update `internal/agent/config.go`
  - [ ] Add LLM API configuration (model, temperature, etc.)
  - [ ] Add retry configuration
  - [ ] Add timeout configuration
  - [ ] Add feature flags

---

## Phase 3: Integration & Testing ⏳ PENDING

### 3.1 Pipeline Integration
- [ ] Integrate intent classifier into main pipeline
- [ ] Connect to input guard (prompt injection detection)
- [ ] Connect to parameter validator
- [ ] Connect to tool executor
- [ ] Add audit logging

### 3.2 Memory Management
- [ ] Update `internal/memory/session.go`
  - [ ] Implement context filtering for dangerous actions
  - [ ] Add session history management
  - [ ] Add context window management
  - [ ] Add memory isolation logic

### 3.3 Evaluation Dataset
- [ ] Create `internal/evaluation/test_cases.json`
  - [ ] 150 GREETING samples
  - [ ] 175 READ_INFO samples
  - [ ] 150 DANGEROUS_ACTION samples
  - [ ] 25 COMPOSITE_ACTION samples
  - [ ] Mix of simple/medium/hard complexity

- [ ] Create `internal/evaluation/evaluator.go`
  - [ ] Load test dataset
  - [ ] Run classification on all samples
  - [ ] Calculate accuracy metrics
  - [ ] Generate confusion matrix
  - [ ] Identify failure patterns

### 3.4 Integration Tests
- [ ] Create `internal/agent/integration_test.go`
  - [ ] Test end-to-end classification flow
  - [ ] Test with real LLM API
  - [ ] Test all intent types
  - [ ] Test error scenarios
  - [ ] Test timeout handling

---

## Phase 4: Evaluation & Optimization ⏳ PENDING

### 4.1 Accuracy Testing
- [ ] Run evaluation on 500+ test dataset
- [ ] Measure overall accuracy (target: >80%)
- [ ] Measure per-class precision (target: >75%)
- [ ] Measure per-class recall (target: >75%)
- [ ] Measure false positive rate for DANGEROUS (target: <5%)
- [ ] Measure false negative rate for DANGEROUS (target: <10%)

### 4.2 Performance Testing
- [ ] Measure average latency (target: <2s)
- [ ] Measure token usage
- [ ] Calculate cost per classification (target: <$0.001)
- [ ] Test with different models (Gemini Flash vs Pro, GPT-4o-mini)
- [ ] Optimize prompt for token efficiency

### 4.3 Optimization
- [ ] Adjust confidence thresholds based on results
- [ ] Add more examples for low-accuracy classes
- [ ] Refine classification rules
- [ ] Optimize prompt wording
- [ ] A/B test different prompt versions

### 4.4 Edge Case Testing
- [ ] Test prompt injection attempts
- [ ] Test vague references ("it", "that", "the file")
- [ ] Test mixed language input
- [ ] Test very long inputs
- [ ] Test malformed inputs
- [ ] Test rapid-fire requests (rate limiting)

---

## Phase 5: Production Deployment ⏳ PENDING

### 5.1 Monitoring & Logging
- [ ] Implement audit logging for all classifications
- [ ] Add metrics collection (accuracy, latency, cost)
- [ ] Set up alerts for low accuracy (<75%)
- [ ] Set up alerts for high error rate (>5%)
- [ ] Create dashboard for monitoring

### 5.2 Rollback Mechanism
- [ ] Implement backup before dangerous actions
- [ ] Add rollback functionality
- [ ] Test rollback for file operations
- [ ] Test rollback for config changes
- [ ] Document rollback procedures

### 5.3 Documentation
- [ ] API documentation
- [ ] User guide for clarification flow
- [ ] Admin guide for monitoring
- [ ] Troubleshooting guide
- [ ] FAQ

### 5.4 Deployment
- [ ] Deploy to staging environment
- [ ] Run smoke tests
- [ ] Monitor for 1 week
- [ ] Fix any issues
- [ ] Deploy to production
- [ ] Monitor production metrics

---

## Phase 6: Continuous Improvement ⏳ PENDING

### 6.1 User Feedback
- [ ] Collect user corrections (when AI misclassifies)
- [ ] Analyze common failure patterns
- [ ] Add failed cases to test dataset
- [ ] Retrain/adjust prompt based on feedback

### 6.2 Model Updates
- [ ] Test new LLM models as they release
- [ ] Compare accuracy and cost
- [ ] Migrate to better models if available

### 6.3 Feature Enhancements
- [ ] Multi-language support (English, Vietnamese, Chinese)
- [ ] Custom intent types (user-defined)
- [ ] Intent prediction (suggest next action)
- [ ] Batch classification for efficiency

---

## Open Questions (Need Answers Before Proceeding)

### Q1: Model Selection
**Question**: Which LLM model should we use for Intent Classifier?

**Options**:
- Gemini 1.5 Pro (high accuracy, higher cost)
- Gemini 1.5 Flash (good accuracy, lower cost) ⭐ RECOMMENDED
- GPT-4o-mini (good accuracy, moderate cost)
- Claude 3.5 Sonnet (high accuracy, higher cost)

**Decision**: ⏳ PENDING

---

### Q2: Evaluation Dataset
**Question**: Do we have existing user conversation logs to create test dataset?

**Options**:
- Use existing logs (if available)
- Manually create synthetic dataset
- Mix of both

**Decision**: ⏳ PENDING

---

### Q3: Multiple Choice UI
**Question**: How should we handle multiple choice clarification?

**Options**:
- Return raw text with options (A/B/C)
- Return structured data for UI to render buttons
- Support both modes

**Decision**: ⏳ PENDING

---

### Q4: Confidence Calibration
**Question**: Should we use LLM logprobs or train a separate confidence classifier?

**Options**:
- Use LLM logprobs (if available)
- Train lightweight BERT-tiny classifier
- Use both (ensemble)

**Decision**: ⏳ PENDING

---

## Success Criteria

### Must Have (P0)
- [x] System prompt created with all safety rules
- [ ] Overall accuracy > 80% on test dataset
- [ ] False positive rate for DANGEROUS < 5%
- [ ] All dangerous actions require confirmation
- [ ] No hallucination for missing parameters

### Should Have (P1)
- [ ] Average latency < 2 seconds
- [ ] Cost per classification < $0.001
- [ ] Composite action detection working
- [ ] Audit logging for all dangerous actions
- [ ] Rollback mechanism for file operations

### Nice to Have (P2)
- [ ] Multi-language support
- [ ] Custom intent types
- [ ] Intent prediction
- [ ] Batch classification

---

## Timeline

| Phase | Duration | Start Date | End Date | Status |
|-------|----------|------------|----------|--------|
| Phase 1: System Prompt | 1 week | 2026-05-24 | 2026-05-31 | ✅ DONE |
| Phase 2: Core Implementation | 2 weeks | 2026-06-01 | 2026-06-14 | ⏳ TODO |
| Phase 3: Integration & Testing | 2 weeks | 2026-06-15 | 2026-06-28 | ⏳ TODO |
| Phase 4: Evaluation & Optimization | 1 week | 2026-06-29 | 2026-07-05 | ⏳ TODO |
| Phase 5: Production Deployment | 1 week | 2026-07-06 | 2026-07-12 | ⏳ TODO |
| Phase 6: Continuous Improvement | Ongoing | 2026-07-13 | - | ⏳ TODO |

**Total Duration**: 7 weeks (excluding continuous improvement)

---

## Risk Assessment

| Risk | Probability | Impact | Mitigation |
|------|-------------|--------|------------|
| LLM accuracy < 80% | Medium | High | Add more examples, adjust thresholds, try different models |
| High latency (>3s) | Low | Medium | Use faster model (Flash), optimize prompt, cache results |
| High cost (>$0.01/call) | Low | Medium | Use cheaper model, reduce prompt size, batch requests |
| Prompt injection bypass | Low | High | Multiple layers of detection, input sanitization |
| False negatives for dangerous actions | Medium | Critical | Lower threshold, add more examples, manual review |

---

## Notes

### Completed Work (2026-05-31)
✅ Created comprehensive system prompt with:
- 4,500 token base prompt
- 5 intent types with detailed descriptions
- 4 critical safety rules
- Confidence scoring algorithm
- 7 detailed examples
- Edge case handling
- Go utilities for prompt building
- Unit tests and benchmarks
- Complete documentation

### Next Immediate Steps
1. Answer open questions (Q1-Q4)
2. Implement `intent_classifier.go` with LLM integration
3. Create evaluation dataset (500+ samples)
4. Run initial accuracy tests

### Contact
For questions or updates, contact: V-Claw Team

---

**Last Updated**: 2026-05-31  
**Updated By**: Kiro AI Assistant

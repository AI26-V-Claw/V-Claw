# 📚 Documentation Overview - V-Claw Project

**Updated:** June 2, 2026 (after master merge)  
**Status:** Sprint 1 - Intent Classification (G3) ✅

---

## 📁 Docs Folder Structure

```
docs/
├── 00-project-brief.md           ← Project overview and goals
├── 01-system-design.md           ← High-level system architecture
├── 02-usecase-diagram.md         ← User interaction diagrams
├── 03-contracts.md               ← Contract specifications for tools, messages, etc.
├── 04-sequences.md               ← Sequence diagrams for workflows (added in master merge)
├── scenarios/
│   ├── 01-channel-message.md     ← Telegram channel integration scenario
│   ├── 02-gmail-read-summary.md  ← Gmail read and summarize use case
│   ├── 03-calendar-create-hitl.md ← Calendar create with HITL approval
│   └── 04-sandbox-command-hitl.md ← Sandbox command execution with HITL
└── tung/
    ├── SEQ-1_Read_Calendar.mmd     ← Sequence diagram: Read calendar events
    ├── SEQ-2_Create_Calendar_FullFlow.mmd  ← Full calendar create workflow
    ├── SEQ-3_Update_Calendar_FullFlow.mmd  ← Full calendar update workflow
    └── SEQ-4_Delete_Calendar_FullFlow.mmd  ← Full calendar delete workflow
```

---

## 📖 Document Descriptions

### Core Architecture

#### **00-project-brief.md**
**Purpose:** Project scope and Sprint 1 goals  
**Key Sections:**
- Project overview (V-Claw: AI-powered Telegram assistant)
- Sprint 1 roadmap (5 goals: G1-G5)
- Team structure (Integration Team, Agent Core Team)
- Success criteria

**Relevant for PR #17:** Yes - defines G3 (Intent Classification) goal

---

#### **01-system-design.md**
**Purpose:** High-level system architecture and component interactions  
**Key Sections:**
- Architecture diagram
- Component responsibilities
- Data flow between modules
- Key design decisions

**Relevant for PR #17:** Yes - describes agent loop and intent classification position in system

---

#### **02-usecase-diagram.md**
**Purpose:** Visual representation of user interactions  
**Key Sections:**
- Actor roles (User, Agent, External Services)
- Use case flows
- System boundaries
- Interaction patterns

**Relevant for PR #17:** Yes - shows how intent classification fits into user interactions

---

#### **03-contracts.md** ⭐ **CRITICAL FOR PR #17**
**Purpose:** Formal specifications for all boundary contracts  
**Key Sections:**

1. **Tool Contract** (section 2.1)
   - Tool naming format: `<domain>.<action>` (e.g., `gmail.sendEmail`, `calendar.createEvent`)
   - Risk levels: SAFE_READ, DANGEROUS_WRITE, COMMUNICATION, EXECUTION
   - Required parameters for each tool

2. **Message Contract** (section 2.2)
   - UserMessage structure and fields
   - AgentResponse structure
   - ToolCall format with name, parameters, timeout
   - ToolResult with status and output

3. **Approval Contract** (section 2.3)
   - ApprovalRequest format
   - RiskLevel enum (NO_RISK, LOW_RISK, MEDIUM_RISK, HIGH_RISK, CRITICAL)
   - ApprovalStatus enum (PENDING, APPROVED, REJECTED, CANCELLED)

4. **Error Contract** (section 2.4)
   - ErrorCode enum for system errors
   - ErrorDetails structure
   - Retry logic and backoff

5. **Agent Response Contract** (section 2.5)
   - ClassificationResult with Intent, Confidence, ToolCalls, NeedsApproval
   - Clarification format when intent ambiguous

**Relevant for PR #17:** ⭐ **ESSENTIAL** - Defines tool naming format `<domain>.<action>` that intent classification must enforce

---

#### **04-sequences.md** (added from master merge)
**Purpose:** Sequence diagrams for main workflows  
**Key Sections:**
- Read Calendar workflow
- Create Calendar workflow
- Update Calendar workflow
- Delete Calendar workflow

**Relevant for PR #17:** Moderate - shows context for where intent classification fits in the flow

---

### Use Case Scenarios

#### **scenarios/01-channel-message.md**
**Purpose:** Telegram channel integration scenario  
**Content:**
- How messages flow from Telegram → V-Claw → Telegram
- Session management
- Channel-specific handling

**Relevant for PR #17:** Moderate - shows message entry point

---

#### **scenarios/02-gmail-read-summary.md**
**Purpose:** Gmail read and summarize use case  
**Content:**
- User asks to read emails
- System fetches emails
- Summarization workflow
- Return to user

**Relevant for PR #17:** Low-Moderate - example of READ_INFO intent

---

#### **scenarios/03-calendar-create-hitl.md**
**Purpose:** Calendar create with human-in-the-loop approval  
**Content:**
- User requests to create calendar event
- System classifies as DANGEROUS_ACTION (needs approval)
- HITL approval flow
- Execution after approval

**Relevant for PR #17:** ⭐ **HIGH** - Demonstrates dangerous action handling that intent classification gates

---

#### **scenarios/04-sandbox-command-hitl.md**
**Purpose:** Sandbox command execution with HITL  
**Content:**
- User requests to run shell/python command
- System classifies as EXECUTION (needs approval + sandboxing)
- Sandbox policy enforcement
- HITL approval and execution

**Relevant for PR #17:** ⭐ **HIGH** - Demonstrates execution action safety requirements

---

### Sequence Diagrams (Mermaid format)

#### **tung/SEQ-1_Read_Calendar.mmd**
**Purpose:** Read calendar events workflow  
**Flow:**
1. User sends "Show me calendar for next week"
2. Intent classifier → READ_INFO
3. No approval needed
4. Fetch calendar events
5. Format and return results

---

#### **tung/SEQ-2_Create_Calendar_FullFlow.mmd**
**Purpose:** Create calendar event with HITL  
**Flow:**
1. User: "Create meeting with John at 2pm tomorrow"
2. Intent classifier → DANGEROUS_ACTION (create event = write operation)
3. Risk assessment: HIGH (external calendar modification)
4. HITL approval requested
5. User approves
6. Create event
7. Confirm to user

**Critical for PR #17:** Shows DANGEROUS_ACTION flow

---

#### **tung/SEQ-3_Update_Calendar_FullFlow.mmd**
**Purpose:** Update calendar event workflow  
**Similar to SEQ-2:** Create, but for update operations

---

#### **tung/SEQ-4_Delete_Calendar_FullFlow.mmd**
**Purpose:** Delete calendar event workflow  
**Flow:** Create/Update pattern, but deletion (highest risk)

**Critical for PR #17:** Shows most dangerous action (deletion)

---

## 🎯 How PR #17 Relates to Documentation

### Key Requirements from Contracts (03-contracts.md):
1. ✅ **Tool naming format:** Intent classifier normalizes to `<domain>.<action>` format
2. ✅ **Risk levels:** Intent classifier identifies DANGEROUS_ACTION, COMMUNICATION, EXECUTION
3. ✅ **ToolCall format:** Intent classifier outputs tool calls matching contract
4. ✅ **Approval gates:** Intent classifier marks when approval is needed
5. ✅ **Clarification:** Intent classifier flags ambiguous intents that need clarification

### Supported Intents (from scenarios + contracts):
- **GREETING**: Responds with acknowledgment
- **READ_INFO**: Safe read operations (no approval)
- **DANGEROUS_ACTION**: Write/delete/modify operations (needs approval)
- **COMPOSITE_ACTION**: Multiple operations, mixed safety levels
- **UNKNOWN**: Unable to classify (needs clarification)

### Tools Recognized (from 03-contracts.md):
```
SAFE_READ:
  - gmail.listEmails
  - gmail.getEmail
  - calendar.listEvents
  - calendar.getEvent

DANGEROUS_WRITE:
  - gmail.sendEmail
  - calendar.createEvent
  - calendar.updateEvent
  - calendar.deleteEvent
  - system.writeFile
  - system.deleteFile

EXECUTION:
  - sandbox.runPython
  - sandbox.runShell

COMMUNICATION:
  - chat.sendMessage
  - telegram.sendMessage
```

---

## 📋 Reading Order

### For Sprint 1 G3 (Intent Classification) Work:
1. **03-contracts.md** ⭐ (Essential - defines what intent classifier must enforce)
2. **01-system-design.md** (Context - where does intent classification fit)
3. **scenarios/03-calendar-create-hitl.md** (Example - dangerous action handling)
4. **scenarios/04-sandbox-command-hitl.md** (Example - execution safety)
5. **tung/SEQ-2_Create_Calendar_FullFlow.mmd** (Visual - dangerous action flow)

### For Broader Project Understanding:
1. **00-project-brief.md** (Goals and team structure)
2. **02-usecase-diagram.md** (User interactions)
3. **04-sequences.md** (Main workflows)
4. **scenarios/** (All use case scenarios)

---

## 🔗 Cross-References

| Document | References | Referenced By |
|----------|-----------|---|
| 00-project-brief.md | - | 01-system-design, ACTIVE_MODULES |
| 01-system-design.md | 00-brief | 03-contracts |
| 02-usecase-diagram.md | 00-brief | scenarios/ |
| **03-contracts.md** | 01-design, 02-usecase | **All internal code**, intent classifier, tools |
| 04-sequences.md | 01-design | scenarios/ |
| scenarios/01-channel.md | 01-design | code: cmd/vclaw, channels/ |
| scenarios/02-gmail.md | 03-contracts | code: tools/gmail |
| scenarios/03-calendar-hitl.md | 03-contracts, 01-design | code: orchestrator, intent classifier |
| scenarios/04-sandbox-hitl.md | 03-contracts, 01-design | code: sandbox, intent classifier |

---

## ✅ Documentation Status

| Document | Updated | Accurate | Status |
|----------|---------|----------|--------|
| 00-project-brief.md | ✓ | ✓ | Current |
| 01-system-design.md | ✓ | ✓ | Current |
| 02-usecase-diagram.md | ✓ | ✓ | Current |
| **03-contracts.md** | ✓ | ✓ | **Current + PR #17 compliant** |
| 04-sequences.md | ✓ (master) | ✓ | Current |
| scenarios/ | ✓ (master) | ✓ | Current |
| tung/ diagrams | ✓ (master) | ✓ | Current |

---

## 🚀 Next Documentation Updates

After PR #17 is merged:

1. **Internal documentation** (in code):
   - Update `internal/agent/intent/README.md` with final accuracy metrics
   - Document prompt injection patterns covered

2. **External documentation**:
   - Add "G3 Completion Report" to docs/
   - Update 00-project-brief.md with G3 completion status
   - Document tool normalization patterns in 03-contracts.md

3. **Decision records**:
   - Consider adding ADR for memory isolation strategy
   - Document prompt injection guard design

---

## 📞 Document Maintenance

- **Owner:** Project lead / Architecture team
- **Review cadence:** Every sprint
- **Update triggers:** Contract changes, architecture decisions, new goals
- **PR #17 impact:** Validates intent classification against 03-contracts.md

---

**Last Updated:** June 2, 2026 (after master merge)  
**Maintained By:** Agent Core Team + Project Lead  
**Version:** Sprint 1 (evolving)

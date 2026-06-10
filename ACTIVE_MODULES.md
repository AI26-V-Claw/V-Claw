# Active Modules & Ownership

> Mục tiêu: giữ kiến trúc triển khai rõ ràng theo 3 tài liệu đã review (`docs/00-project-brief.md`, `docs/01-system-design.md`, `docs/02-usecase-diagram.md`), giúp các nhóm phát triển độc lập nhưng vẫn thống nhất các điểm bắt buộc.  
> Folder có tồn tại không có nghĩa là được phép implement tùy ý ngoài roadmap/scope đã review.

---

## 1. Nguyên tắc chung

- Ưu tiên **vertical slice chạy được** trước khi mở rộng kiến trúc.
- Mỗi nhóm được tự chủ trong phạm vi module của mình.
- Những thay đổi vào vùng shared hoặc ngoài ownership cần giải thích rõ trong PR.
- Không thêm abstraction/framework/layer mới nếu chưa phục vụ trực tiếp cho roadmap/use case đã review.
- Các module phục vụ Sprint sau có thể tồn tại trong repo nhưng chỉ implement khi có task/approval rõ ràng theo roadmap.

---

## 2. Nhóm phụ trách

> Mapping theo `docs/00-project-brief.md`: **Integration Team** bao gồm phần Tích hợp API và Phương thức kết nối; **Agent Core Team** bao gồm phần Agent & Bộ nhớ và Hệ thống & Sandbox.

### Integration Team

Phụ trách tích hợp API và các phương thức kết nối.

Bao gồm:

- Google Workspace connectors: Gmail, Calendar, Chat.
- Google Workspace connectors: Drive, Docs, Sheets theo MVP read-first.
- Telegram/Slack/channel adapters.
- OAuth/config/secrets liên quan external services.
- Mock/fake adapters cho external APIs.
- Tool implementation cho các thao tác external API.

### Agent Core Team

Phụ trách hệ thống agent, sandbox, HITL và memory.

Bao gồm:

- Agent loop.
- Intent/risk classification.
- Tool routing.
- HITL approval flow.
- Sandbox execution policy.
- Short-term memory/session context.
- Long-term memory/Knowledge Graph theo Sprint 3.
- Audit/risk logging ở boundary thực thi tool.

---

## 3. Active Modules cho roadmap hiện tại

Các module dưới đây được phép implement theo đúng sprint/task tương ứng trong roadmap đã review.

### 3.1. Entry point & App wiring

| Module | Owner chính | Ghi chú |
|---|---|---|
| `cmd/` | Shared | Entry point mỏng, chỉ parse config/args và gọi app bootstrap. Không chứa business logic nặng. |
| `internal/config/` | Shared | Load config tối giản cho local/dev. Không biến thành config framework phức tạp. |
| `internal/app/` | Shared | Nếu cần, dùng để gom wiring/bootstrap. Tránh để `cmd/` phình to. |

### 3.2. Shared contracts & persistence

| Module | Owner chính | Ghi chú |
|---|---|---|
| `internal/contracts/` | Shared | Runtime objects bắt buộc: `UserMessage`, `AgentResponse`, `ToolCall`, `ToolResult`, `RiskDecision`, `ApprovalRequest`, `ErrorCode`, `RiskLevel`. |
| `internal/store/` | Shared | Persistence tối giản, database access, migrations nếu có. |
| `internal/store/migrations/` | Shared | PostgreSQL schema/migration. Cần Lead review vì ảnh hưởng nhiều nhóm. |
| `internal/store/repositories/` | Shared | Repository tối giản cho session/message/tool_call/approval/audit nếu cần. |

### 3.3. Agent Core modules

| Module | Owner chính | Ghi chú |
|---|---|---|
| `internal/agent/` | Agent Core | Agent loop, planning đơn giản, xử lý `UserMessage` thành response/tool calls. |
| `internal/agent/intent/` | Agent Core | Intent classification tối giản. Không overfit bằng nhiều layer rule engine nếu chưa cần. |
| `internal/agent/router/` | Agent Core | Tool routing: chọn tool dựa trên intent/context. |
| `internal/providers/` | Agent Core | LLM provider interface hoặc implementation tối giản. |
| `internal/memory/` | Agent Core | Short-term memory ở Sprint 2; long-term memory/Knowledge Graph theo Sprint 3. |
| `internal/memory/session/` | Agent Core | Lưu/đọc context hội thoại hiện tại nếu cần. |
| `internal/memory/longterm/` | Agent Core | Bộ nhớ dài hạn theo Sprint 3, ví dụ thói quen làm việc và danh sách người quen. |

### 3.4. Tools, connectors & channels

| Module | Owner chính | Ghi chú |
|---|---|---|
| `internal/tools/` | Shared | Agent-facing tool interface, registry tối giản và wrappers. |
| `internal/tools/registry/` | Shared | Danh sách tool, input/output shape, default risk level. Cần Lead review khi đổi. |
| `internal/tools/office/gmail/` | Integration | Agent-callable Gmail tools, ví dụ `listEmails`, `getEmail`, `listThreads`, `getThread`, draft tools, attachment download, `modifyMessage`. |
| `internal/tools/office/calendar/` | Integration | Agent-callable Calendar tools, ví dụ `listEvents`, `createEvent`, `deleteEvent`. |
| `internal/tools/office/chat/` | Integration | Agent-callable Google Chat tools theo roadmap Google Workspace. |
| `internal/tools/office/drive/` | Integration | Agent-callable Drive tools, ví dụ `searchFiles`, `getFileMetadata`, `exportFile`, `downloadFile`, `createTextFile`, `updateTextFile`, `renameFile`, `shareFile`. |
| `internal/tools/office/docs/` | Integration | Agent-callable Google Docs tools, ví dụ `getDocument`, `createDocument`, `appendText`. |
| `internal/tools/office/sheets/` | Integration | Agent-callable Google Sheets tools, ví dụ `getSpreadsheet`, `listSheets`, `readRange`, `createSpreadsheet`, `updateRange`, `appendRows`. |
| `internal/tools/system/` | Agent Core | Agent-callable local/system tools đi qua sandbox/safety. |
| `internal/connectors/` | Integration | Raw API clients/adapters cho external services. Không chứa agent reasoning. |
| `internal/connectors/google/` | Integration | Gmail/Calendar/Chat raw clients, OAuth/API response handling. |
| `internal/connectors/google/gmail/` | Integration | Gmail API client. |
| `internal/connectors/google/calendar/` | Integration | Calendar API client. |
| `internal/connectors/google/chat/` | Integration | Google Chat API client theo roadmap Google Workspace. |
| `internal/connectors/google/drive/` | Integration | Google Drive API client cho file metadata, export/download, create/update/share. |
| `internal/connectors/google/docs/` | Integration | Google Docs API client cho read/create/append. |
| `internal/connectors/google/sheets/` | Integration | Google Sheets API client cho metadata/range read/write/append. |
| `internal/channels/` | Integration | User-facing adapters: Telegram/Slack. |
| `internal/channels/telegram/` | Integration | Kênh giao tiếp với Agent theo Sprint 1. |
| `internal/channels/slack/` | Integration | Kênh giao tiếp với Agent theo Sprint 1. |

### 3.5. Safety, HITL & sandbox

| Module | Owner chính | Ghi chú |
|---|---|---|
| `internal/safety/` | Agent Core | Risk classification, allow/deny/approval decision. |
| `internal/safety/risk/` | Agent Core | RiskLevel, policy mapping, side-effect classification. |
| `internal/safety/policy/` | Agent Core | Policy check tối giản. Không tách rule engine phức tạp sớm. |
| `internal/approvals/` | Agent Core | Pending approval, approve/reject flow. |
| `internal/approvals/pending/` | Agent Core | Lưu trạng thái action đang chờ duyệt nếu cần. |
| `internal/audit/` | Agent Core | Log action, approval decision, tool execution result. |
| `internal/sandbox/` | Agent Core | Python/Shell execution trong môi trường kiểm soát. |
| `internal/sandbox/runner/` | Agent Core | Runner thực thi lệnh/script. |
| `internal/sandbox/policy/` | Agent Core | Chính sách cho phép/chặn command/file access. |

### 3.6. Tests, fixtures & docs

| Module | Owner chính | Ghi chú |
|---|---|---|
| `tests/` | Shared | Unit, contract, safety và E2E fixture tests. |
| `tests/contracts/` | Shared | Test đảm bảo tool/input/output contract không bị phá. |
| `tests/safety/` | Agent Core | Test risk/HITL/sandbox policy. |
| `tests/e2e/` | Shared | 1-2 scenario chính cho vertical slice. |
| `tests/fixtures/` | Shared | Mock data dùng chung giữa Integration và Agent Core. |
| `docs/` | Shared | Architecture, contracts, workflow, ERD. |
| `docs/adr/` | Shared | Quyết định kiến trúc đã chốt nếu cần. |

---

## 4. Shared Areas cần Lead review

Các thay đổi trong nhóm này cần được giải thích rõ trong PR và nên có review từ Lead hoặc đại diện cả 2 nhóm.

### 4.1. Contract & schema

| Area | Vì sao cần review kỹ |
|---|---|
| `internal/contracts/` | Ảnh hưởng trực tiếp cả Integration và Agent Core. |
| ToolCall/ToolResult format | Boundary chính giữa agent và tool. |
| ApprovalRequest format | Boundary chính của HITL. |
| RiskLevel/ErrorCode enum | Ảnh hưởng HITL và safety behavior. |
| Tool Registry | Agent Core và Integration cùng phụ thuộc. |

### 4.2. Data & testing

| Area | Vì sao cần review kỹ |
|---|---|
| Database schema/migrations | Dễ phá dữ liệu, audit, approval flow. |
| E2E fixtures | Là tiêu chuẩn pass/fail chung cho vertical slice. |
| Contract tests | Là cơ chế chống drift giữa 2 nhóm. |

### 4.3. Safety boundary

| Area | Vì sao cần review kỹ |
|---|---|
| Sandbox policy | Liên quan bảo mật và destructive actions. |
| Approval gate | Nếu sai, tool có thể thực thi side effect khi chưa được duyệt. |
| Audit format | Cần trace được action đã approve/reject/execute. |

---

## 5. Frozen Modules cho đến khi có approval

Các module dưới đây không nên được implement trong giai đoạn đầu nếu chưa có task/approval rõ ràng.

### 5.1. Platform/extension nâng cao

| Module | Trạng thái | Lý do |
|---|---|---|
| `internal/mcp/` | Frozen | Chưa cần cho MVP nếu tool interface nội bộ đã đủ. |
| `internal/eventbus/` | Frozen | Chỉ cần khi có async/multi-worker thực sự. |
| `internal/orchestration/` | Frozen | Tránh tạo layer phức tạp trước khi agent loop ổn định. |
| `internal/pipeline/` | Frozen | Chỉ tách khi agent loop lớn lên. |
| `internal/localapi/` | Frozen | Chỉ cần khi có API server/local daemon rõ ràng. |

### 5.2. Memory/storage nâng cao

| Module | Trạng thái | Lý do |
|---|---|---|
| `internal/vault/` | Frozen | Secret/vault nâng cao, chỉ làm khi có requirement rõ. |
| `internal/cache/` | Frozen | Tránh tối ưu sớm. |
| `internal/backup/` | Frozen | Thuộc vận hành/sprint sau. |

### 5.3. UI/ops nâng cao

| Module | Trạng thái | Lý do |
|---|---|---|
| `internal/desktop/` | Frozen | UI desktop không thuộc MVP core. |
| `internal/tracing/` | Frozen | Observability nâng cao, chưa bắt buộc ở MVP. |
| `internal/upgrade/` | Frozen | Không cần khi chưa có release flow. |

---

## 6. Boundary rule cho `connectors` và `tools`

Để tránh duplicate logic giữa 2 nhóm:

```text
connectors = raw external API clients
tools      = agent-callable operations
```

Ví dụ:

```text
internal/connectors/google/gmail/client.*
  - gọi Gmail API thật
  - xử lý OAuth/API response
  - không biết agent là gì

internal/tools/gmail/list_emails.*
  - nhận ToolCall input
  - gọi Gmail connector
  - trả ToolResult
  - khai báo risk level
```

Agent Core chỉ gọi tool interface, không gọi trực tiếp Google/Slack SDK.

---

## 7. HITL/Safety invariant

Mọi action có side effect phải đi qua một approval/safety boundary duy nhất.

Các action cần approval bao gồm tối thiểu:

- Gửi email/chat message ra bên ngoài.
- Tạo/sửa/xóa Calendar event.
- Ghi/sửa/xóa file local.
- Chạy Python/Shell command.
- Bất kỳ destructive hoặc irreversible action nào.

Không tool nào được tự thực thi side effect nếu chưa có `RiskDecision` và approval hợp lệ khi cần.

---

## 8. Quy tắc PR tối thiểu

Mỗi PR nên trả lời các câu hỏi sau:

```md
## Scope
- [ ] Tôi chỉ sửa module thuộc ownership của nhóm mình.
- [ ] Nếu có sửa shared/out-of-scope module, tôi đã giải thích lý do.

## Contract impact
- [ ] Không thay đổi contract.
- [ ] Có thay đổi contract và đã cập nhật docs/fixtures liên quan.

## Safety impact
- [ ] Không ảnh hưởng HITL/risk/sandbox.
- [ ] Có ảnh hưởng HITL/risk/sandbox và đã mô tả rõ.

## Tests
- [ ] Đã thêm/cập nhật test phù hợp.
- [ ] Đã chạy test liên quan trước khi request review.

## Security hygiene
- [ ] Không in/paste access token, refresh token, client secret trong log, test output, PR comment.
- [ ] File secrets local (`configs/google/credentials.json`, `configs/google/token.json`) không được commit.
```

---

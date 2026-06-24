# Review Sprint3/N1-Bao: Agent Context Assembly

## Phạm vi review

Task được review:

> Context đưa vào agent gồm system prompt chuẩn, transcript gần nhất, summary,
> memory liên quan, kết quả tool gần nhất và nguồn tham chiếu; system prompt
> được viết lại rõ vai trò, giới hạn, tool policy, memory rule và HITL; thông
> tin nhạy cảm được hạn chế; agent vẫn tiếp tục đúng khi context dài hoặc bị
> nén.

Nhánh đã cải thiện rõ ràng thứ tự context, cấu trúc system prompt, redaction
cho một số nguồn dữ liệu và continuity sau compaction. Tuy nhiên, các finding
dưới đây cần được xử lý trước khi xem task là hoàn thiện.

## Finding 1 - High: System prompt mâu thuẫn contract về sensitive read

`internal/agent/runtime_prompt.go` ghi rằng:

```text
Read-only actions execute directly.
```

Trong khi `docs/03-contracts.md` quy định `gmail.getEmail` là
`sensitive_read` và yêu cầu approval. Câu hiện tại có thể khiến LLM hiểu rằng
mọi read tool đều được chạy trực tiếp, dù runtime policy vẫn có thể chặn tool
call sau đó.

Đề xuất sửa:

```text
Safe read actions may execute directly.
Sensitive reads and all side-effect actions must follow tool policy and
approval requirements.
```

System prompt nên mô tả đúng Tool Registry và không tự định nghĩa lại risk
behavior theo cách rộng hơn contract.

## Finding 2 - High: Chưa có token budget tổng cho context

### Vấn đề

`compactProviderTranscriptForPrompt` hiện giới hạn transcript ở 12 message và
cắt mỗi tool message xuống 1.600 bytes. Cơ chế này giới hạn số lượng message,
nhưng không giới hạn tổng số token thực tế được gửi cho provider.

Một user hoặc assistant message trong 12 message gần nhất vẫn có thể chứa hàng
chục nghìn token. Ngoài transcript, context còn bao gồm:

- System prompt.
- Tool definitions.
- Toàn bộ `USER.md`.
- Một phần `NOTES.md`.
- Session summary.
- Recent action results.
- Resolved references.
- Linked knowledge.
- Active plan và các system message bổ sung.

Vì vậy context vẫn có thể vượt context window dù transcript chỉ còn 12
message. Hậu quả có thể là provider trả `context length exceeded`, mất phần
context quan trọng, tăng latency hoặc tăng chi phí.

### Hướng xử lý đề xuất

Tạo một context budget assembler dùng token estimate thay vì chỉ dùng số
message. Budget cần được tính từ `Runtime.contextWindow` và chừa dung lượng cho
output cùng các vòng tool continuation.

Ví dụ với context window 128.000 token:

| Thành phần | Budget đề xuất |
|---|---:|
| Output và tool continuation reserve | 20.000 |
| System prompt và tool schemas | Đo thực tế, luôn giữ |
| Long-term memory | Tối đa 8.000 |
| Session summary | Tối đa 4.000 |
| References và linked knowledge | Tối đa 6.000 |
| Recent action results | Tối đa 8.000 |
| Recent transcript | Phần budget còn lại |

Các con số nên cấu hình được và không hard-code theo riêng model 128k.

### Thứ tự ưu tiên khi thiếu budget

1. Luôn giữ system prompt và tool policy.
2. Luôn giữ current user message.
3. Giữ tool protocol sequence đang active và pending HITL/clarification.
4. Giữ tool result mới nhất cần cho continuation.
5. Giữ session summary.
6. Giữ transcript từ mới về cũ đến khi hết budget.
7. Cắt linked knowledge, references và long-term memory theo relevance.

### Hướng triển khai

1. Thêm cấu trúc `ContextBudget` hoặc helper tương đương nhận:
   `contextWindow`, output reserve và budget từng section.
2. Dùng `sessions.EstimateMessagesTokens`/`sessions.EstimateTokens` để đo từng
   phần trước khi append vào provider messages.
3. Chọn transcript từ message mới nhất đi ngược về trước cho đến khi hết
   budget, thay vì luôn lấy đúng 12 message.
4. Truncate user/assistant message quá lớn, không chỉ tool message.
5. Giới hạn cả `USER.md`; giữ heading và các fact liên quan query trước.
6. Giới hạn linked knowledge và reference context theo token, không chỉ số item.
7. Log số token ước lượng của từng section để quan sát và tuning.
8. Nếu vẫn vượt budget, compact đồng bộ hoặc trả lỗi ổn định thay vì để
   provider tự từ chối request.

### Test cần bổ sung

- Một user message lớn hơn transcript budget vẫn không làm request vượt context.
- `USER.md` rất lớn được cắt nhưng fact liên quan current request vẫn còn.
- Current user message và pending approval không bao giờ bị loại.
- Context sau compaction vẫn chứa summary và recent transcript trong budget.
- Tổng token estimate của provider messages nhỏ hơn:
  `contextWindow - outputReserve`.
- Context window khác nhau, ví dụ 32k và 128k, đều phân bổ đúng.

## Finding 3 - Medium: Redaction chưa bao phủ toàn bộ context

`redactSensitiveForPrompt` đã được áp dụng cho summary, recent action results,
long-term memory và resolved reference context. Tuy nhiên, nó chưa được áp
dụng nhất quán cho:

- Recent transcript.
- Linked knowledge.
- File path và Drive ID trong reference source block.

Redaction theo từng dòng cũng chưa xử lý an toàn toàn bộ PEM block: dòng
`BEGIN PRIVATE KEY` có thể bị xóa nhưng các dòng base64 tiếp theo vẫn còn.

Đề xuất:

- Dùng một sanitizer chung cho mọi context section trước khi gửi provider.
- Redact nguyên khối PEM từ `BEGIN` đến `END`.
- Phân loại path/ID nào thực sự cần cho LLM thay vì inject mặc định.
- Có test end-to-end kiểm tra toàn bộ `ChatRequest.Messages`, không chỉ từng
  helper riêng lẻ.

## Finding 4 - Medium: `promptVersion` không ổn định như contract mô tả

`NewRuntime` gọi:

```go
runtimeSystemPrompt(time.Time{})
```

với mục tiêu loại datetime động khỏi fingerprint. Nhưng
`runtimeSystemPrompt` lại thay zero time bằng `time.Now()`. Vì vậy hai Runtime
được tạo ở hai thời điểm khác nhau có thể nhận `promptVersion` khác nhau dù
phần system prompt tĩnh không đổi.

Đề xuất:

- Tách system prompt thành phần tĩnh và datetime runtime.
- Hash chỉ phần tĩnh, hoặc truyền một timestamp cố định khi tính version.
- Thêm test tạo hai Runtime ở hai thời điểm khác nhau và xác nhận
  `promptVersion` giống nhau.
- Thêm test xác nhận thay đổi nội dung prompt tĩnh làm version thay đổi.

## Finding 5 - Medium: Provenance mới chỉ ở mức tên tool

Reference source block hiện chỉ cung cấp thông tin dạng:

```text
- tool result from gmail.listEmails
```

Trong khi `sessions.ActionResult` chỉ lưu `ToolName`, `Content` và
`CreatedAt`. Nó chưa lưu:

- `toolCallId`.
- `requestId` hoặc `runId`.
- `source`.
- `artifactRef`.
- Resource ID/link an toàn.

Nếu cùng một tool được gọi nhiều lần, agent không xác định được một fact đến
từ lần gọi nào. Đây mới là source label, chưa phải provenance đầy đủ.

Đề xuất:

- Mở rộng `ActionResult` theo hướng backward-compatible với các field
  provenance cần thiết.
- Lưu provenance trực tiếp khi `recordActionResult` nhận `ToolResult`.
- Reference block nên gắn result với resource/link hoặc tool call cụ thể khi
  an toàn.
- Không hiển thị opaque ID cho end user; provenance này chỉ phục vụ context,
  trace và audit.
- Bổ sung test với hai lần gọi cùng tool nhưng kết quả/resource khác nhau.

## Kết luận

Nhánh đã hoàn thành phần lớn context ordering và prompt hardening, nhưng cần
xử lý ít nhất Finding 1, Finding 2 và Finding 4 trước khi merge. Finding 3 và
Finding 5 nên được xử lý để đáp ứng đầy đủ yêu cầu về hạn chế dữ liệu nhạy cảm
và nguồn tham chiếu có thể truy vết.
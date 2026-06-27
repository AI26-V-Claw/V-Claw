# Review: feature/Multi-tools-flow

Ngay review: 2026-06-27

Pham vi: chi tinh task dau tien:

- Agent chay duoc cac luong ket hop nhu Gmail -> Calendar -> Chat, Drive -> Docs -> Sheets, Web Search -> Docs.
- Cac buoc doc / de xuat / ghi duoc tach ro.
- Hanh dong ghi, sua, chia se phai di qua HITL approval.

Khong tinh task workflow dinh ky trong review nay, vi user da xac nhan nhanh nay moi lam task dau tien.

## Ket Luan Nhanh

Nhanh nay moi dung o muc partial.

Da co them prompt va guard `ValidateReadBeforeWrite` de huong LLM doc truoc khi ghi. Nen ve mat demo nhe, mot so multi-step flow co the chay tot hon. Tuy nhien implementation hien tai van chua du chat de coi la hoan thanh task dau tien, vi guard con hard-code sai/ thieu tool, chan ca batch hop le, va tu tat sau 2 lan nudge.

Nen sua truoc khi merge.

## Findings Can Sua

### 1. High - Khong nen hard-code danh sach read/write tool

File: `internal/agent/workspace_flow_helpers.go`

Hien tai `workspaceReadTools` va `workspaceWriteTools` tu duy tri danh sach tool bang string literal.

Van de:

- Danh sach nay bi drift so voi `docs/03-contracts.md` va registry that.
- Thieu nhieu tool quan trong:
  - Read: `calendar.getEvent`, `drive.listPermissions`, `web.fetch`, `sheets.batchGetValues`.
  - Write/local/destructive: `drive.saveFile`, `drive.updateFileMetadata`, `drive.shareFile`, `drive.revokePermission`, `drive.trashFile`, `drive.untrashFile`, `docs.insertText`, `docs.deleteContent`, `sheets.batchUpdateValues`, `sheets.appendValues`, `sheets.clearValues`, `sheets.addSheet`, `sheets.renameSheet`, `sheets.deleteSheet`, `sheets.duplicateSheet`, `gmail.updateDraft`, `gmail.deleteDraft`, `gmail.downloadAttachments`, `meet.createMeeting`.

Impact:

- Guard read-before-write khong bat duoc nhieu write/share/delete co rui ro.
- Code moi co the lech contract ngay khi tool registry thay doi.

De xuat:

- Dung `ToolRegistry` lam source of truth.
- Phan loai theo `Capability`, `RiskLevel`, `RequiresApproval`, va group/namespace neu can gioi han Google Workspace.
- Neu bat buoc giu map Workspace-only, phai dung constants tu package tool va them test drift so voi registry.

### 2. High - Read-before-write validation dang chan ca batch hop le

File: `internal/agent/runtime.go`

Runtime goi `ValidateReadBeforeWrite(assistantMessage.ToolCalls, toolResults)` truoc khi execute bat ky tool nao trong batch.

Van de:

- Neu LLM tra ve cung luc `web.search` + `docs.createDocument`, guard van thay `toolResults` rong va nudge lai.
- Tuong tu voi `drive.listFiles` + `docs.createDocument` hoac `gmail.listEmails` + `calendar.createEvent`.
- Trong khi contract/prompt lai khuyen generate cac tool calls cung luc neu buoc sau khong phu thuoc ID chua biet.

Impact:

- Multi-tool flow bi ton iteration khong can thiet.
- LLM co the bi day vao retry loop hoac hanh vi fallback khong mong muon.

De xuat:

- Validate theo thu tu trong cung batch.
- Cho phep read tool dung truoc write trong batch.
- Neu write phu thuoc ket qua read chua co ID/content, runtime nen execute read truoc roi yeu cau LLM continuation, khong reject toan batch.

### 3. High - Guard tu tat sau 2 lan nudge nen invariant khong chac

File: `internal/agent/runtime.go`

Hien co:

```go
readBeforeWriteNudges := 0
const readBeforeWriteNudgeLimit = 2
```

Sau 2 lan nudge, guard khong con chan nua.

Van de:

- Neu task yeu cau read/propose/write tach ro, guard khong nen tu dong cho qua sau vai lan LLM khong nghe.
- Write truoc read van co the di tiep sau khi vuot nudge limit.

De xuat:

- Sau nudge limit, tra `need_clarification`, `blocked`, hoac `failed` voi ly do ro.
- Khong cho write di tiep neu flow bat buoc doc truoc ghi.
- Neu la create-from-scratch hop le thi nen detect intent ngay tu dau thay vi dua vao nudge limit.

### 4. Medium - Can phan biet create-from-scratch voi write dua tren workspace data

File: `internal/agent/workspace_flow_helpers.go`

Van de:

- Khong phai moi write deu can read truoc.
- Vi du user noi day du: "Tao Google Doc voi noi dung X" hoac "Tao lich hop title Y tu 10h den 11h" thi co the khong can read.
- Nhung flow `Gmail -> Calendar -> Chat`, `Drive -> Docs -> Sheets`, `Web Search -> Docs` thi write phai dua tren output doc truoc do.

De xuat:

- Guard nen dua tren dependency/intent thay vi blanket "workspace write nao cung can read".
- Co the phan loai:
  - Write from explicit user input: khong bat buoc read.
  - Write from external/source data: bat buoc co fresh successful read cung run.
  - Write can target resource ID/name: bat buoc resolve bang read tool truoc.

### 5. Medium - `docs.createDocument` bao success du content append fail

File: `internal/tools/office/docs/tool.go`

Nhanh them input `content` cho `docs.createDocument`. Service tao document truoc, sau do goi `AppendText`.

Van de:

- Neu `AppendText` loi, code set `document.BodyText = "[content append failed: ...]"` roi return success.
- Voi flow `Web Search -> Docs`, user co the tuong document da co noi dung trong khi append fail.

De xuat:

- Neu `content` la mot phan yeu cau chinh, append fail thi ToolResult nen `success=false`.
- Hoac tra partial result co metadata ro, vi du `partial=true`, `appendFailed=true`, va final answer phai noi ro.
- Khong nen nhung raw provider error vao `BodyText` nhu content tai lieu.

### 6. Medium - Them input `content` cho `docs.createDocument` nhung chua cap nhat contract docs

File: `internal/tools/office/docs/tool.go`

Van de:

- Tool schema da doi: `docs.createDocument` co them optional `content`.
- Day la thay doi behavior/schema tool, can cap nhat `docs/03-contracts.md` hoac docs tool contract lien quan.

De xuat:

- Cap nhat contract/docs neu chap nhan field `content`.
- Them contract test de dam bao schema moi on dinh.

### 7. Medium - Chua co test dung cac flow task yeu cau

Hien test moi chu yeu cover helper doc lap va fake-provider path don gian.

Can them test cho:

- `gmail.listEmails -> gmail.getEmail -> calendar.createEvent approval -> chat.sendMessage approval`.
- `drive.listFiles -> docs.createDocument approval -> sheets.createSpreadsheet` hoac `sheets.appendValues approval`.
- `web.search -> web.fetch -> docs.createDocument approval`.
- Batch hop le co read truoc write trong cung assistant response.
- Write/share/delete bi thieu trong map hien tai van phai qua HITL: `drive.shareFile`, `docs.deleteContent`, `sheets.clearValues`, `drive.trashFile`.

### 8. Low - `SummarizeFlowSteps` hien chua duoc runtime/channel dung

File: `internal/agent/workspace_flow_helpers.go`

Function `SummarizeFlowSteps` co test nhung khong thay duoc tich hop vao response, audit, hay progress output.

De xuat:

- Neu can "read/propose/write tach ro" cho user/channel, nen su dung summary nay de render progress/result.
- Neu khong dung, nen bo de giam dead code.

## Danh Gia Theo Task

### Gmail -> Calendar -> Chat

Partial.

Runtime da co co che approval continuation va remaining tool calls tu truoc. Nhanh nay them prompt/guard de khuyen read-before-write. Nhung chua co test rieng chung minh flow Gmail -> Calendar -> Chat chay dung, dung fresh Gmail data, tao Calendar sau approval, roi gui Chat sau approval.

### Drive -> Docs -> Sheets

Partial.

Co prompt Docs moi va guard read-before-write. Nhung helper thieu nhieu Drive/Docs/Sheets write tools, nen chua bao phu dung contract. Chua co test flow nay.

### Web Search -> Docs

Partial.

Prompt da huong dung `web.search` va `web.fetch`, nhung helper chi xem `web.search` la read, thieu `web.fetch`. `docs.createDocument` content append fail van co the bao success. Chua co test flow nay.

### Tach read / de xuat / write

Partial/weak.

Guard hien tai chi la system nudge, khong phai enforcement chac. No khong biet dependency that, chan batch hop le, va tu tat sau 2 lan.

### HITL cho write/sua/chia se

Mostly OK o policy/registry hien co.

`go test ./...` pass va policy van bat approval cho mutating/destructive tools. Tuy nhien helper read/write moi bi drift, nen phan "flow guard" khong bao phu du cac write/share/delete trong contract.

## Verification

Da chay:

```powershell
go test ./...
```

Ket qua: pass toan bo.

Luu y: working tree luc review co 2 file Telegram modified local:

- `internal/channels/telegram/bot.go`
- `internal/channels/telegram/bot_test.go`

Hai thay doi nay chi tat Telegram link preview va co test tuong ung, khong tinh vao finding cua nhanh `feature/Multi-tools-flow`.

## De Xuat Sua Toi Thieu Truoc Khi Merge

1. Thay hard-coded read/write maps bang registry-driven classification hoac constants + drift tests.
2. Sua validator de chap nhan read-before-write trong cung batch.
3. Bo co che "nudge 2 lan roi cho qua"; thay bang fail/blocked/clarify neu invariant van bi vi pham.
4. Phan biet create-from-scratch voi write dua tren external read data.
5. Sua `docs.createDocument` partial failure.
6. Cap nhat docs contract cho `docs.createDocument.content`.
7. Them fake-provider tests cho 3 flow chinh cua task.

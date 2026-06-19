param(
  [string]$Prefix = $(if ($env:VCLAW_E2E_PREFIX) { $env:VCLAW_E2E_PREFIX } else { "[VCLAW-E2E]" })
)

$ErrorActionPreference = "Stop"

@"
Seed fixtures chưa tự động tạo object thật trong skeleton này.

Prefix: $Prefix

Cần chuẩn bị hoặc cung cấp env selectors:
- VCLAW_E2E_TARGET_EMAIL
- VCLAW_E2E_SECONDARY_EMAIL (nếu cần)
- VCLAW_E2E_CALENDAR_ID
- VCLAW_E2E_DRIVE_FOLDER_ID
- VCLAW_E2E_CHAT_SPACE (nếu test Chat)
- VCLAW_E2E_CHAT_MEMBER_EMAIL (nếu test Chat member)

Mọi object fixture/write phải chứa prefix '$Prefix' và run_id khi scenario chạy.
"@

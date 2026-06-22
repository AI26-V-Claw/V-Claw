# V-Claw E2E local env
# File này nằm trong testing-e2e/ và đã được gitignore.
# Sửa các giá trị bên dưới rồi chạy:
#   . ./testing-e2e/e2e.env.ps1
# hoặc để các script tự load file này qua -EnvFile.

$env:VCLAW_E2E_PREFIX="[VCLAW-E2E]"
$env:VCLAW_E2E_TARGET_EMAIL="baolnc@vclaw.site"
$env:VCLAW_E2E_CALENDAR_ID="primary"
$env:VCLAW_E2E_DRIVE_FOLDER_ID="1weZue2gBeWpa3MufJAevX06A31uETUwu"
$env:VCLAW_E2E_CHAT_SPACE="spaces/3fCq5yAAAAE"
$env:VCLAW_E2E_SECONDARY_EMAIL="quanghtd@vclaw.site"
$env:VCLAW_E2E_CHAT_MEMBER_EMAIL="hainx@vclaw.site"
$env:VCLAW_E2E_PEOPLE_QUERY=""

# Optional judge; không bắt buộc cho hard assertions.
$env:OPENAI_API_KEY="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"

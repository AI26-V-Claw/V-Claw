# V-Claw Windows Quickstart

1. Doi ten `.env.example` thanh `.env` va dien cac bien can thiet.
2. Neu dung Google Workspace, dat `credentials.json` vao `configs/google/`.
3. Chay `google-auth.bat` de dang nhap Google lan dau.
4. Chay `start.bat` de mo runtime.
5. Chay `status.bat` de kiem tra trang thai.

Toi thieu can dien trong `.env`:
- `OPENAI_API_KEY`
- `OPENAI_MODEL`
- `TELEGRAM_BOT_TOKEN`
- `ALLOWED_TELEGRAM_USER_ID`

Neu dung `status/logs/approvals` day du, can cau hinh them `DATABASE_URL` va chay Postgres.

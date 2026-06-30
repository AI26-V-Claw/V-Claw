@echo off
cd /d %~dp0
set HAS_DATABASE_URL=
set OPENAI_API_KEY=
set LLM_API_KEY=
set OPENAI_MODEL=
set LLM_MODEL=
set OPENAI_BASE_URL=
set LLM_BASE_URL=
set TELEGRAM_BOT_TOKEN=
set VCLAW_TELEGRAM_BOT_TOKEN=
set ALLOWED_TELEGRAM_USER_ID=
set VCLAW_TELEGRAM_ALLOWED_USER_IDS=
set DATABASE_URL=
set VCLAW_GOOGLE_CREDENTIALS_PATH=
set VCLAW_GOOGLE_TOKEN_PATH=
set VCLAW_GOOGLE_TOOLS_MODE=
set VCLAW_WEB_TOOLS_MODE=
set TAVILY_API_KEY=
set TALIVY_API_KEY=
set TAVILY_BASE_URL=
set VCLAW_SANDBOX_IMAGE=
set VCLAW_SANDBOX_WORKSPACE_DIR=
set VCLAW_SKILL_CACHE_DIR=
set VCLAW_SKILL_NUDGE_INTERVAL=
set VCLAW_PARALLEL_ENABLED=
set VCLAW_PARALLEL_MAX_WORKERS=
set VCLAW_PARALLEL_TOOL_TIMEOUT=
set METRICS_PORT=
set LANGFUSE_PUBLIC_KEY=
set LANGFUSE_SECRET_KEY=
set LANGFUSE_HOST=
set LANGFUSE_PROJECT_ID=
if not exist .env (
  copy /Y .env.example .env >nul 2>nul
  echo .env chua duoc cau hinh. Vui long chay setup truoc.
  call setup.bat
  exit /b 1
)
for /f "tokens=1,* delims==" %%A in (.env) do (
  if /I "%%A"=="DATABASE_URL" set HAS_DATABASE_URL=%%B
)
if defined HAS_DATABASE_URL (
  docker --version >nul 2>nul
  if errorlevel 1 (
    echo Docker Desktop chua duoc cai hoac chua mo.
    echo V-Claw se tiep tuc, nhung neu can database thi hay mo Docker truoc.
  ) else (
    docker compose up -d postgres >nul 2>nul
  )
)
if not exist logs mkdir logs
set LOG_FILE=logs\vclaw-runtime.log
echo V-Claw dang khoi dong...
echo.
echo Sau khi bot san sang, Telegram se tu mo.
echo Ban se nhan tin va dung bot trong Telegram, khong phai trong cua so nay.
echo.
echo Neu co loi, nhat ky ky thuat se nam o:
echo %LOG_FILE%
echo.
start "" cmd /c "ping -n 6 127.0.0.1 >nul & tasklist | findstr /I /C:"vclaw.exe" >nul && call open-telegram.bat"
vclaw.exe telegram run --google-tools auto --web-tools auto 1>>"%LOG_FILE%" 2>&1
if errorlevel 1 (
  echo V-Claw da dung do loi.
  echo Mo file %LOG_FILE% de xem chi tiet ky thuat.
)
pause

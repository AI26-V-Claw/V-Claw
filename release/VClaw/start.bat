@echo off
cd /d %~dp0
copy /Y .env.example .env >nul 2>nul
vclaw.exe telegram run --google-tools auto --web-tools auto
pause

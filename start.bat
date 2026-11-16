@echo off
setlocal
cd /d "%~dp0"
chcp 65001 >nul

if exist "twitch-social-bot.exe" (
    "twitch-social-bot.exe" %*
) else (
    go run ./cmd/bot %*
)

if errorlevel 1 (
    echo.
    echo [Twitch Social Bot] Process exited with an error.
    pause
)

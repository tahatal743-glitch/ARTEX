@echo off
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "F:\ARTEX\Stop-ARTEX.ps1"
if errorlevel 1 pause

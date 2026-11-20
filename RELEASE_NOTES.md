## Twitch Social Bot v2.1.2

### Improvements & Bug Fixes / Mejoras y Correcciones

- **Automatic `.exe.old` Cleanup on Windows Update / Limpieza Automática de `.exe.old` en Windows**
  - **EN:** Updated Windows `Relaunch()` to spawn the updated binary in its own interactive console (`cmd /c start`) so the replaced `.exe.old` process exits immediately instead of blocking while waiting on the child process. Added background retry logic to `CleanupOldBinary()` so leftover `.exe.old` files are deleted cleanly right after updating.
  - **ES:** Se actualizó `Relaunch()` en Windows para abrir el nuevo binario en su propia consola interactiva (`cmd /c start`) y permitir que el proceso viejo (`.exe.old`) se cierre de inmediato sin retener el bloqueo del archivo. Además, `CleanupOldBinary()` ahora reintenta brevemente al arrancar hasta borrar el `.exe.old` automáticamente tras actualizar.

- *(Included from `v2.1.1`)* **Same-Account Broadcaster Mode Fix / Soporte para Misma Cuenta (`Broadcaster == Bot`)**
  - Allows triggered `!bot ...` / `@bot ...` prompts and admin commands from the broadcaster when using the same Twitch account as both streamer and bot, with a `60s` sent-reply cache to prevent echo loops.

---

### How to Update / Cómo Actualizar
- **From v2.1.1 / v2.1.0 / v2.0.0 / v1.x:** Open `twitch-social-bot.exe` and press **Enter** on **Update now and restart** (**Actualizar ahora y reiniciar**)—your `.env`, `.tokens.json`, `personas/`, and `data/` memory are preserved automatically. *(Note: if you are updating from `v2.1.1`, the `v2.1.1` parent process still uses the old wait behavior during that transition; `v2.1.2` will clean up `.exe.old` cleanly on future updates and normal restarts.)*
- **New users:** Download `twitch-social-bot-windows.zip` below, extract it, and double-click `twitch-social-bot.exe`.

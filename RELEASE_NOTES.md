## Twitch Social Bot v2.1.1

### Bug Fix / Corrección de Error

- **Same-Account Broadcaster Mode Fix / Soporte para Misma Cuenta (Streamer = Bot)**
  - **EN:** Fixed an issue where running the bot on the same Twitch account as the broadcaster (`BroadcasterID == BotUserID`) caused all `!bot ...` / `@bot ...` prompts and admin commands sent by the streamer to be silently dropped by the self-message filter. Trigger detection now runs before filtering self-messages, allowing triggered broadcaster messages in same-account mode while still ignoring untriggered chat and dedicated-bot self-messages.
  - **ES:** Corregido un error por el cual, al usar la misma cuenta de Twitch para el canal y el bot (`BroadcasterID == BotUserID`), el filtro de auto-mensajes descartaba todos los mensajes del streamer (`!bot hola`, `@bot ...`, `!bot status`, etc.) antes de detectar el trigger. Ahora los mensajes del broadcaster que incluyen el prefijo o mención del bot se procesan correctamente.

- **Echo-Loop Guard / Protección Anti-Bucles**
  - **EN:** Added a short-lived (`60s`) cache of sent bot replies (`sentReplies`) so Twitch EventSub echoes are ignored in same-account mode even if a generated reply mentions `@botname`.
  - **ES:** Se añadió una caché temporal (`60s`) de respuestas enviadas para ignorar ecos devueltos por Twitch EventSub en modo misma cuenta y evitar bucles infinitos.

---

### How to Update / Cómo Actualizar
- **From v2.1.0 / v2.0.0 / v1.x:** Open `twitch-social-bot.exe` and press **Enter** on **Update now and restart** (**Actualizar ahora y reiniciar**)—your `.env`, `.tokens.json`, `personas/`, and `data/` memory are preserved automatically.
- **New users:** Download `twitch-social-bot-windows.zip` below, extract it, and double-click `twitch-social-bot.exe`.

## Twitch Social Bot v2.3.1

### What's New / Novedades

- **Interactive Live Broadcast Desk CLI & Speaking Ring / Panel Animado en Vivo y Speaking Ring**
  - **EN:** While running in interactive mode, the bot now renders a live terminal dashboard instead of static log lines: displays the active persona's truecolor Braille avatar (`AvatarForPersona`) surrounded by a reactive **Speaking Ring** (`[  IDLE  ]` → `[ THINK  ]` → `[ ON AIR ]` / `[ SPEAK  ]`), a live Braille neural/audio equalizer, real-time message/reply counters, and the latest `[IN]` / `[BOT]` interaction. Switching personas mid-stream (`!bot persona ...`) updates the avatar, theme color, and labels automatically.
  - **ES:** Mientras el bot está en ejecución en modo interactivo, ahora muestra un panel animado en vivo sin emojis ni descuadres: avatar Braille real de la persona activa (compatible con las 4 personas base, personalizadas y tarjetas V2/V3 importadas) rodeado de un **Speaking Ring** reactivo (`[  IDLE  ]` → `[ THINK  ]` → `[ ON AIR ]`), ecualizador Braille en tiempo real, contadores de mensajes/respuestas y la última interacción `[IN]` / `[BOT]`.

- **Instant Live `[LOGS]`, `[P]` Persona Switcher & Return to Start Menu / Modo `[LOGS]`, Selector `[P]` y Regreso al Menú**
  - **EN:** Standard runtime logs are captured cleanly in a 200-line ring buffer (`LogBuffer`) so they never corrupt the UI. Press **`L`** at any time to switch to `[LOGS]` for live debugging, press **`P`** to choose and switch personas with arrow keys + **`Enter`** without disconnecting Twitch (saved automatically to `.env`), press **`Q` or `Esc`** to return straight to the start menu, or **`Ctrl+C`** to exit. Headless `--run` mode keeps classic stderr logging unchanged.
  - **ES:** Los logs en tiempo real se capturan en un buffer circular de 200 líneas sin romper el dibujo. Presiona **`L`** para ver **`[LOGS]`** en vivo, presiona **`P`** para elegir y cambiar de personalidad con flechas + **`Enter`** sin desconectar Twitch (se guarda en `.env`), presiona **`Q` o `Esc`** para regresar al menú de inicio, o **`Ctrl+C`** para salir. El modo `--run` conserva los logs clásicos por consola.

---

### How to Update / Cómo Actualizar
- **From v2.2.x / v2.1.x / v2.0.0 / v1.x:** Open `twitch-social-bot.exe` and press **Enter** on **Update now and restart** (**Actualizar ahora y reiniciar**)—your `.env`, `.tokens.json`, custom `personas/`, and `data/` memory are preserved automatically.
- **New users:** Download `twitch-social-bot-windows.zip` below, extract it, and double-click `twitch-social-bot.exe`.

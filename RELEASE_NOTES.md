## Twitch Social Bot v2.3.2

### What's New / Novedades

- **Archetype-Neutral `SOUL.md` / `SOUL.es.md` & Clean Prompt Separation / `SOUL` Neutral al Arquetipo y Prompts Limpios**
  - **EN:** Upgraded `SOUL.md` and `SOUL.es.md` to community standards (OpenClaw / ElizaOS / SillyTavern) tailored for Twitch live chat: removed sarcasm/banter bias so solemn, romantic, or imported V2/V3 personas keep 100% of their own voice, added anti-`*asterisk*` roleplay narration, anti-interviewer, and unbreakable immersion boundaries, and removed duplicate style rules from the hard security block in `locales/*.yaml`. Updated `docs/personas.md` and `docs/personas.es.md` to match the real modular v2.3 schema.
  - **ES:** Actualizamos `SOUL.md` y `SOUL.es.md` al estándar de la comunidad (OpenClaw / ElizaOS / SillyTavern) adaptado a chat en vivo de Twitch: eliminamos el sesgo obligatorio a sarcasmo/vacile para que Dante, Celeste o tarjetas V2/V3 importadas mantengan 100% su propia voz, añadimos reglas anti-`*asteriscos*`, anti-entrevistador e inmersión blindada, y quitamos duplicados en `locales/*.yaml`. También actualizamos `docs/personas.es.md` y `docs/personas.md` con el esquema modular real de `v2.3`.

- **New CLI Settings Menu & Global `Q` / `Esc` Navigation / Menú de Configuración y `Q` / `Esc` en Todo el CLI**
  - **EN:** Added a dedicated **Settings** submenu in the CLI to toggle **18+ (NSFW)** on/off, adjust **Global Cooldown**, **Per-User Cooldown**, **Max Viewer Facts**, and **AI Timeout** directly (persisted immediately to `.env`), plus full access to the API & Twitch `.env` wizard. Enabled **`Q` / `Esc`** across every CLI menu to go back or exit cleanly, and updated the header card to display live `NSFW` and cooldown values.
  - **ES:** Nuevo submenú **Configuración** en el CLI para activar/desactivar **Filtro +18 (NSFW)**, ajustar **Cooldown Global**, **Cooldown por Usuario**, **Hechos por Espectador** y **Timeout de IA** directo (guardando al instante en `.env`), además del asistente de API y Twitch. Habilitamos **`Q` / `Esc`** en todos los menús del CLI para volver o salir, y la tarjeta principal ahora muestra `NSFW` y cooldowns en vivo.

- **Windows Self-Updater `.old` Cleanup Fix / Corrección de Limpieza `.old` en Windows**
  - **EN:** Switched Windows relaunch to non-blocking `cmd.Start()` so the replaced `.exe.old` process exits immediately and releases its file lock, extended background cleanup retries to ~15s, and added automatic sweeping of any leftover `*.old*` binaries in the installation folder.
  - **ES:** El reinicio tras actualizar en Windows ahora usa `cmd.Start()` (no bloqueante) para que el proceso `.exe.old` suelte el bloqueo de archivo al instante, ampliamos los reintentos de limpieza en segundo plano a ~15s y limpiamos automáticamente cualquier residuo `*.old*` en la carpeta del bot.

---

### How to Update / Cómo Actualizar
- **From v2.3.x / v2.2.x / v2.1.x / v2.0.0 / v1.x:** Open `twitch-social-bot.exe` and press **Enter** on **Update now and restart** (**Actualizar ahora y reiniciar**)—your `.env`, `.tokens.json`, custom `personas/`, and `data/` memory are preserved automatically.
- **New users:** Download `twitch-social-bot-windows.zip` below, extract it, and double-click `twitch-social-bot.exe`.

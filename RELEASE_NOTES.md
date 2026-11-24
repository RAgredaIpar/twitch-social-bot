## Twitch Social Bot v2.3.3

### What's New / Novedades

- **Explicit Persona `gender` & Grounded Character Identity / Género Explícito en Personalidades**
  - **EN:** Added `gender` support across `PersonaSpec`, YAML loader/serializer, the `+ Create New Persona` CLI wizard, and LLM system prompts (`Gender:`). Pre-configured built-in personas (`Mochi`, `Celeste`, `Lyra` as Female; `Dante` as Male) and refined `SOUL.md` / `SOUL.es.md` so characters stay 100% faithful to their identity instead of dodging gender questions as "genderless digital entities".
  - **ES:** Agregamos soporte de `gender` en `PersonaSpec`, el cargador/serializador YAML, el asistente `+ Crear Nueva Personalidad` y el prompt al LLM (`Género:`). Configuramos las personalidades base (`Mochi`, `Celeste` y `Lyra` femenino; `Dante` masculino) y ajustamos `SOUL.es.md` / `SOUL.md` para que el personaje responda fiel a su identidad sin evadir preguntas sobre su género.

- **Full-Line Word Wrap & Scroll in `[LOGS]` View / Logs Completos con Word-Wrap y Scroll en `[LOGS]`**
  - **EN:** Replaced `...` truncation in the live `[LOGS]` debug screen with clean multi-line word wrapping (preserving timestamp grouping and error/info colors) plus `↑ / ↓` scrolling across the 200-line log buffer with a `(+N)` scroll indicator.
  - **ES:** Eliminamos el recorte con `...` en la vista `[LOGS]`: ahora los mensajes y errores largos se dividen en varias líneas completas sin perder color ni alineación, y puedes hacer scroll con `↑ / ↓` sobre las últimas 200 líneas con indicador `(+N)`.

---

### How to Update / Cómo Actualizar
- **From v2.3.x / v2.2.x / v2.1.x / v2.0.0 / v1.x:** Open `twitch-social-bot.exe` and press **Enter** on **Update now and restart** (**Actualizar ahora y reiniciar**)—your `.env`, `.tokens.json`, custom `personas/`, and `data/` memory are preserved automatically.
- **New users:** Download `twitch-social-bot-windows.zip` below, extract it, and double-click `twitch-social-bot.exe`.

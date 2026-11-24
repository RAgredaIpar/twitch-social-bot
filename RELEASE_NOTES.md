## Twitch Social Bot v2.2.1

### What's New & Improvements / Novedades y Mejoras

- **Passive Memory & Anti-Topic-Loop / Memoria Pasiva Anti-Calzador**
  - **EN:** Added `SelectContextualFacts` to cool down `Facts` the bot just mentioned in the previous reply when the viewer's current message is about something else (while keeping them whenever the viewer actually mentions or asks about that topic). Added `[PASSIVE MEMORY]` guidance and `DetectRepeatedTermsForTurn` to stop unprompted 2-turn topic loops without hardcoding banned topics.
  - **ES:** Se añadió `SelectContextualFacts` para enfriar temporalmente los `Facts` recién mencionados en el turno anterior cuando el espectador cambia de tema (manteniéndolos siempre si el usuario pregunta o habla de ellos). Se añadió la directiva `[MEMORIA PASIVA]` y `DetectRepeatedTermsForTurn` para frenar temas repetidos en 2 turnos seguidos sin calzar datos viejos en conversaciones que no tienen relación.

- **Persona Attribution in Conversation History / Identidad por Personaje al Cambiar de Persona**
  - **EN:** `ConversationTurn` now records `BotName` and `PersonaID` (`omitempty`, 100% backward-compatible with existing `data/social.db`). When switching personas mid-stream (e.g. Mochi ↔ Celeste ↔ Dante), recent turns are labeled with the speaker's actual name and inject a style-isolation guard so the new persona keeps context without copying the previous persona's tone.
  - **ES:** `ConversationTurn` ahora guarda `BotName` y `PersonaID` (compatible con tu `data/social.db` actual). Al cambiar de personalidad en vivo, el historial etiqueta quién respondió (`Mochi:`, `Celeste:`, `Dante:`) y protege la voz del personaje activo para no contagiarse del tono anterior.

- **Balanced Mochi Banter & Complicity / Balance entre Vacile y Complicidad en Mochi**
  - **EN:** Refined `personas/cohost_amigos.yaml` (`bio`, `style`, `message_examples`) so Mochi alternates sharp banter with genuine co-host complicity during long streams.
  - **ES:** Se afinó `personas/cohost_amigos.yaml` para que Mochi alterne su humor filoso con complicidad genuina y charla de chill en streams largos.

---

### How to Update / Cómo Actualizar
- **From v2.2.0 / v2.1.x / v2.0.0 / v1.x:** Open `twitch-social-bot.exe` and press **Enter** on **Update now and restart** (**Actualizar ahora y reiniciar**)—your `.env`, `.tokens.json`, custom `personas/`, and `data/` memory are preserved automatically.
- **New users:** Download `twitch-social-bot-windows.zip` below, extract it, and double-click `twitch-social-bot.exe`.

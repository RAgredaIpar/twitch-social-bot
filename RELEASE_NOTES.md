## Twitch Social Bot v2.2.0

### What's New & Fixes / Novedades y Correcciones

- **1-Account & 2-Account Echo Shield / Blindaje Anti-Eco y Triggers (1 y 2 Cuentas)**
  - **EN:** Captures Twitch's real outgoing `message_id` from `POST /helix/chat/messages` (`TrackedChatSender`), routes all admin command replies (`!bot status`, `set`, `pause`, `resume`, `persona`, `reset`) through `sendTrackedReply`, and strips Twitch's threaded `@mention` prefix during echo detection. In same-account mode (`Broadcaster == Bot`), `@<BotLogin>` is excluded as a trigger so viewer mentions to the streamer or threaded echoes never misfire the bot (while `!bot` and `@<BotName>` stay active; 2-account mode keeps `@<BotLogin>` active).
  - **ES:** Ahora se captura el `message_id` real de Twitch al responder (`TrackedChatSender`), todos los comandos admin pasan por `sendTrackedReply` y se ignora la `@mención` inicial que agrega Twitch en respuestas en hilo. En modo 1 cuenta (`Broadcaster == Bot`), `@<BotLogin>` deja de actuar como trigger para que ni los ecos de `!bot status` ni los viewers mencionando al streamer disparen al bot por error.

- **Faster Cooldowns (`8s` Viewers / Broadcaster Bypass) / Cooldown Ágil (`8s` Viewers y Bypass para Streamer)**
  - **EN:** Lowered default `USER_COOLDOWN_MS` from `15000` (`15s`) to **`8000` (`8s`)** for viewers, and updated `CooldownLimiter` (`AllowForUser`) so the channel broadcaster bypasses the per-viewer cooldown (keeping only the `3s` global cooldown).
  - **ES:** El cooldown por defecto para espectadores bajó de `15s` a **`8s`** (`USER_COOLDOWN_MS=8000`) y el streamer del canal ahora solo respeta el cooldown global (`3s`) para conversar fluido sin mensajes ignorados.

- **Dynamic Anti-Loop Lexical Engine, Tighter Pacing & Channel Name / Anti-Bucle Dinámico, Ritmo y Nombre del Canal**
  - **EN:** Added `DetectRepeatedTerms` to dynamically detect overused filler words or edge tags (e.g. `xd`, `bro`, `p`, `jaja`, `lit`) across recent bot replies without hardcoding banned words. Replaced `"Channel Owner"` / `"Dueño del canal"` with the actual streamer/channel name, added initial streamer familiarity context, rebalanced pacing (`50%` short / `42%` single-sentence / `8%` max 2 short sentences), and restricted follow-up questions to `10%` on medium turns only.
  - **ES:** Nuevo detector dinámico `DetectRepeatedTerms` que frena temporalmente cualquier muletilla repetida en los últimos turnos sin quitar `"xd"` ni hardcodear filtros. Se reemplazó `"Dueño del canal"` por el nombre real del canal/streamer, se ajustó el ritmo (`50%` corto / `42%` 1 frase / `8%` máx. 2 frases cortas) y se bajaron las preguntas finales a `10%` solo en turnos medios.

- **Expanded Natural Social-Media Dialogue Examples / Ejemplos Conversacionales Ampliados**
  - **EN:** Expanded `message_examples` in Mochi (`cohost_amigos.yaml`, 8 examples) and the other 3 built-in presets (`drama_romance.yaml`, `clan_jerarquia.yaml`, `rpg_facciones.yaml`, 6 examples each) with natural Twitch/social-media phrasing.
  - **ES:** Se ampliaron los `message_examples` de Mochi (8 ejemplos con jerga natural de Twitch/redes y el cierre coloquial `"p"`) y de Celeste, Dante y Lyra (6 ejemplos cada uno).

---

### How to Update / Cómo Actualizar
- **From v2.1.x / v2.0.0 / v1.x:** Open `twitch-social-bot.exe` and press **Enter** on **Update now and restart** (**Actualizar ahora y reiniciar**)—your `.env`, `.tokens.json`, custom `personas/`, and `data/` memory are preserved automatically. *(Tip: if your existing `.env` still has `USER_COOLDOWN_MS=15000`, change it to `8000` to use the new viewer cooldown.)*
- **New users:** Download `twitch-social-bot-windows.zip` below, extract it, and double-click `twitch-social-bot.exe`.

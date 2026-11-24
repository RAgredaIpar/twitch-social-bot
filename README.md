![Twitch Social Bot](docs/assets/banner.webp)

# Twitch Social Bot

Conversational chat agent with persistent social memory, dynamic viewer affinity, human-like chat pacing, and hot-swappable YAML personas designed for live Twitch interactive streams.

Engineered in **pure Go** (100% standard library, zero external dependencies, zero CGO), supporting **DeepSeek, Claude (Anthropic), ChatGPT (OpenAI), Google Gemini, Groq, Grok (xAI), and Custom/Private APIs**, and connected directly to Twitch EventSub via WebSocket.

English | [Español](README.es.md)

---

## Features

- **Pure Go Standard Library**: Zero external dependencies in `go.mod`. The entire runtime stack (HTTP server, WebSocket client, YAML parser, JSON serializer, ANSI/Braille image converter, auto-updater, and OAuth flow) uses solely the Go standard library.
- **Interactive Modern CLI (`↑`/`↓` + `Enter`)**:
  - **Guided First-Time Onboarding**: Automatically walks new users step-by-step through language selection, AI provider + API key, Twitch Developer App registration, bot account setup, and initial persona.
  - **Per-Persona Braille Avatars & Theme Colors**: Displays a crisp `28×12` (`56×48` subpixel) Unicode Braille avatar and custom CLI border accent color per persona (`Mochi` Emerald, `Celeste` Pink, `Lyra` Cyan, `Dante` Red).
  - **Built-In Persona Creator**: Create new personalities directly inside the CLI (`+ Create New Persona`), including dragging and dropping any `.png`/`.jpg`/`.gif` image to convert it to Braille art on the fly.
- **Multi-AI Provider Engine & Live Model Discovery**:
  - Supports **DeepSeek**, **Claude (Anthropic)** (`/v1/messages`), **ChatGPT (OpenAI)**, **Google Gemini**, **Groq**, **Grok (xAI)**, and **Custom / Private APIs** (OpenRouter, Ollama, vLLM, etc.).
  - Automatically queries `GET /models` using your API key during setup to pick the latest suitable chat model without hardcoding, with safe fallbacks and a 3-state Circuit Breaker (`Closed`, `Open`, `Half-Open`).
- **Multi-Language i18n (`locales/*.yaml`)**:
  - Switchable between **English (`en`)** and **Spanish (`es`)** (and extensible via custom YAML files in `locales/`), localizing LLM system prompts, bot chat replies, and the CLI interface.
- **Human-Like Conversational Behavior**:
  - **Variable Turn Pacing & Minimal Replies**: Matches ultra-short viewer messages (`"hola"`, `"xd"`, `"f"`) with snappy 1–4 word reactions and varies reply length naturally across turns.
  - **Anti-Repetition & Decisive Stance**: Uses `frequency_penalty`/`presence_penalty`, anti-cliché rules (no robotic AI assistant filler), and forces clear opinions with zero hedging.
  - **Dynamic Session Mood**: Adapts between `chill`, `hype`, `annoyed`, and `playful` based on recent chat interactions with 10-minute decay.
  - **Twitch Hierarchy & Live Stream Context**: Reads Twitch badges (`Broadcaster`, `Moderator`, `VIP`, `Subscriber`, `New Viewer`) and fetches the current stream game/category and title via Twitch Helix.
- **Persistent Social Relationship Engine & Competitive Slots**:
  - Individual affinity (`-100` to `+100`), dynamic tiers, earned titles, episodic viewer facts (default: 15), and competitive social slots with outrank-stealing mechanics.
- **Hot-Swappable YAML Personas**:
  - Detailed catalog and authoring guide: [Persona Catalog (English)](docs/personas.md) | [Catálogo de Personalidades (Español)](docs/personas.es.md).
  - Built-in presets: `Mochi` (`cohost_amigos.yaml`), `Celeste` (`drama_romance.yaml`), `Lyra` (`rpg_facciones.yaml`), and `Dante` (`clan_jerarquia.yaml`).
- **Mandatory Auto-Updater (Git & GitHub Releases)**:
  - Automatically checks for newer versions at startup and prompts users to update and restart seamlessly while preserving `.env`, `.tokens.json`, and `data/`.
- **Twitch Native Transport, Safety & Diagnostics**:
  - EventSub WebSocket (`channel.chat.message`), OAuth 2.0 Device Code Flow (`twitch.tv/activate`), strict prompt-injection & output guardrails, rate limiters, and `/healthz` + `/status` HTTP endpoints.

---

## Architecture

```
                      +----------------------------------+
                      |        Twitch Chat Room          |
                      +-----------------+----------------+
                                        |
                             EventSub WebSocket Frame
                                        v
+--------------------------------------------------------------------------------+
| Orchestrator (internal/engine)                                                 |
|                                                                                |
|   1. Self-message filter & Twitch badge extraction (Broadcaster/Mod/VIP/Sub)   |
|   2. Trigger prefix detection (!bot, @BotName, @BotLogin)                      |
|        +-- NO TRIGGER --> Ambient Room Buffer (internal/room)                  |
|        `-- TRIGGERED                                                           |
|             +-- Admin Command (Broadcaster/Moderator) -> Deterministic Handler |
|             `-- Conversational Pipeline:                                       |
|                  +-- Rate Limiter Check (internal/ratelimit)                   |
|                  +-- Safety Guardrail Inspection (internal/safety)             |
|                  +-- Profile, Mood & Cached Helix Stream Context               |
|                  +-- Localized Prompt + Variable Pacing (internal/llm)         |
|                  +-- Multi-Provider Inference (OpenAI-compat / Anthropic)      |
|                  +-- Social Transition State Machine (internal/social)         |
|                  +-- Atomic Disk Flush (internal/store)                        |
|                  `-- Threaded Chat Reply (internal/twitch)                     |
+--------------------------------------------------------------------------------+
        |                                                                |
        v                                                                v
+-----------------------+                                      +-----------------+
| HTTP Gateway          |                                      | YAML Personas & |
| (/healthz, /status)   |                                      | Locales (i18n)  |
+-----------------------+                                      +-----------------+
```

---

## Chat Commands

### Public Commands
- `!bot <message>` or `@BotName <message>`: Interacts with the active persona. Evaluates affinity deltas, records episodic memories, and consumes user cooldown allowances.

### Moderation & Administrative Commands
Restricted to Broadcaster and Channel Moderators. These commands bypass conversational rate limits and execute deterministically:

| Command | Arguments | Description |
|---|---|---|
| `!bot pause` | None | Temporarily suspends conversational replies. |
| `!bot resume` | None | Resumes standard conversational processing. |
| `!bot status` | None | Reports active persona, runtime health, and slot holders to chat. |
| `!bot persona` | `<persona_id>` | Hot-swaps the operational personality (e.g., `!bot persona rpg_facciones`). |
| `!bot set` | `@user <affinity>` | Overrides viewer affinity score (`-100` to `+100`), recomputing tier and slot assignments. |
| `!bot reset` | `@user` | Purges episodic memory, resets affinity to baseline, and clears active cooldowns for the target user. |

---

## Quickstart

### Option A: Windows Ready-to-Use Release (No Go or Git Required)
1. Download `twitch-social-bot-windows.zip` from [GitHub Releases](https://github.com/RAgredaIpar/twitch-social-bot/releases).
2. Extract the folder and double-click **`twitch-social-bot.exe`**.
3. Follow the interactive onboarding tutorial using your arrow keys (`↑`/`↓` + `Enter`).

### Option B: From Source (Developers)
- **Prerequisites**: **Go 1.22+** and an API key from your chosen AI provider (plus an optional Twitch Client ID from [dev.twitch.tv/console](https://dev.twitch.tv/console)).
```bash
git clone https://github.com/RAgredaIpar/twitch-social-bot.git
cd twitch-social-bot
go run ./cmd/bot
```
- **Flags**:
  - `--run`: Skip the interactive CLI and start the bot directly (used in Docker/servers).
  - `--no-update`: Skip the startup update check.
  - `--env <path>`: Load/save a custom `.env` file path.

On initial Twitch launch without saved tokens, the terminal initiates the OAuth 2.0 Device Code Flow:
```text
=================================================================
TWITCH AUTHORIZATION REQUIRED
Visit: https://www.twitch.tv/activate
Code: WXYZ-1234
=================================================================
```
Open `https://www.twitch.tv/activate` **signed into your bot's Twitch account**, enter the code, and grant authorization. Tokens are saved to `.tokens.json` (git-ignored) and refreshed automatically.

---

## Container Deployment

A multi-stage Docker build is available producing a minimal, rootless static binary (defaults to `--run` headless mode):

```bash
# Build container image
make docker-build

# Run container with volume persistence
docker run -d \
  --name twitch-social-bot \
  --restart unless-stopped \
  --env-file .env \
  -p 8080:8080 \
  -v $(pwd)/data:/app/data \
  twitch-social-bot:latest
```

---

## Make Targets

```bash
make test          # Executes test suite with race detector enabled (-race)
make test-coverage # Generates test execution profile and coverage report
make build         # Compiles static executable binary to bin/bot
make run           # Executes application locally
make clean         # Removes generated binaries, test artifacts, and coverage profiles
make fmt           # Runs gofmt across all repository packages
```

---

## Configuration Reference

| Variable | Type | Default | Description |
|---|---|---|---|
| `BOT_LANGUAGE` | String | `en` | Active language pack (`en`, `es`, or custom file in `locales/`). |
| `LOCALE_DIR` | String | `locales` | Directory containing YAML language packs. |
| `LLM_PROVIDER` | String | `deepseek` | AI provider (`deepseek`, `claude`, `openai`, `gemini`, `groq`, `xai`, `custom`). |
| `LLM_API_KEY` | String | *(Required)* | API authentication key for the selected AI provider. |
| `LLM_BASE_URL` | String | `https://api.deepseek.com` | Base endpoint URL for LLM completions. |
| `LLM_MODEL` | String | `deepseek-chat` | Model identifier (auto-discovered via `GET /models` in the CLI). |
| `LLM_TIMEOUT_MS` | Integer | `8000` | Upstream LLM request timeout in milliseconds. |
| `TWITCH_CLIENT_ID` | String | `""` | Registered Twitch Developer Application Client ID. |
| `TWITCH_BROADCASTER_LOGIN`| String | `""` | Twitch username of the broadcaster channel. |
| `TWITCH_BOT_LOGIN` | String | `""` | Twitch username representing the chat bot account. |
| `TWITCH_ACCESS_TOKEN` | String | `""` | Initial OAuth access token override (optional). |
| `TWITCH_REFRESH_TOKEN` | String | `""` | Initial OAuth refresh token override (optional). |
| `TWITCH_TOKEN_FILE` | String | `.tokens.json` | Local filepath for persistence of OAuth tokens. |
| `PERSONA_DIR` | String | `personas` | Directory containing declarative YAML persona definitions. |
| `PERSONA_FILE` | String | `personas/cohost_amigos.yaml` | Initial YAML persona loaded at startup. |
| `STORE_PATH` | String | `data/social.db` | Local filesystem path for the atomic JSON social database. |
| `GLOBAL_COOLDOWN_MS` | Integer | `3000` | Global broadcast cooldown between bot messages in milliseconds. |
| `USER_COOLDOWN_MS` | Integer | `8000` | Per-viewer rate limit cooldown in milliseconds. |
| `ROOM_BUFFER_SIZE` | Integer | `25` | Maximum sliding window capacity of ambient chat messages retained. |
| `MAX_VIEWER_FACTS` | Integer | `15` | Default maximum quantity of episodic facts stored per viewer. |
| `ROOM_BUFFER_MAX_AGE_SEC` | Integer | `600` | Maximum age in seconds before ambient chat messages expire. |
| `HTTP_ADDR` | String | `127.0.0.1:8080` | Network address and port for the diagnostics HTTP gateway. |

---

## Diagnostic Endpoints

### `GET /healthz`
```json
{
  "status": "ok"
}
```

### `GET /status`
```json
{
  "paused": false,
  "active_persona_id": "cohost_amigos",
  "active_persona_name": "Co-Host Amigos",
  "bot_name": "Mochi",
  "trigger_prefix": "!bot",
  "active_slots": {
    "favorito_del_bot": [
      {
        "slot_id": "favorito_del_bot",
        "user_id": "12345678",
        "display_name": "ViewerOne",
        "affinity": 85,
        "assigned_at": "2026-11-12T04:30:00Z"
      }
    ]
  },
  "processed_messages": 420,
  "generated_replies": 35
}
```

---

## License

This project is licensed under the terms of the [MIT License](LICENSE).

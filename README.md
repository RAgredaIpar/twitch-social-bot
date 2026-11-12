# Twitch Social Bot

Conversational chat agent with persistent social memory, dynamic viewer affinity, and hot-swappable YAML personas designed for live Twitch interactive streams.

Engineered in pure Go (100% standard library, zero external dependencies, zero CGO), backed by DeepSeek models with prefix caching optimizations, and connected directly to Twitch EventSub via WebSocket.

English | [Español](README.es.md)

---

## Features

- **Pure Go Standard Library**: No external third-party dependencies in `go.mod`. The entire runtime stack (HTTP server, WebSocket client, YAML parser, JSON serializer, rate limiters, and OAuth flow) uses solely the Go standard library.
- **Persistent Social Relationship Engine**:
  - Individual affinity score tracking bounded from `-100` to `+100` per viewer.
  - Dynamic relationship tiers modifying tone, familiarity, and decision boundaries.
  - Earned titles awarded dynamically based on cumulative conversational context.
  - Episodic memory tracking viewer facts (configurable default: 15 items, FIFO eviction, deduplication).
  - Multi-turn conversation memory retaining recent dialogue exchanges per participant.
- **Competitive Social Slots**:
  - Limited-capacity honorary positions (e.g., Best Friend, Nemesis, Right Hand).
  - Deterministic qualification and automatic revocation based on threshold boundaries.
  - Outrank stealing mechanics enabling higher-affinity viewers to displace incumbents.
- **Hot-Swappable YAML Personas**:
  - Declarative configuration files located in the `personas/` directory.
  - Detailed catalog and authoring guide: [docs/personas.md](docs/personas.md) ([Español](docs/personas.es.md)).
  - Dynamic runtime switching via chat commands (`!bot persona <id>`) with zero process restarts.
  - Production presets provided:
    - `cohost_amigos.yaml`: Warm, witty stream co-host.
    - `rpg_facciones.yaml`: Faction guardian with allegiance and guild ranking mechanics.
    - `clan_jerarquia.yaml`: Tribal hierarchy with respect-based challenges.
    - `drama_romance.yaml`: Interactive narrative drama with emotional stakes.
- **DeepSeek Integration with Prefix Caching & Circuit Breaker**:
  - Deterministic system prompts structured for DeepSeek context caching to minimize latency and token expenditure.
  - Three-state circuit breaker (`Closed`, `Open`, `Half-Open`) ensuring failure isolation against upstream outages.
- **Twitch Native Transport**:
  - Low-latency bidirectional EventSub communication via WebSocket (`channel.chat.message`).
  - Terminal-based OAuth 2.0 Device Code Flow (RFC 8628) removing the need for public webhook ingress or local callback listeners.
  - Threaded replies dispatched through Twitch Helix API.
- **Safety, Moderation, and Rate Control**:
  - Strict input sanitization preventing command injection and prompt leakage.
  - Output filtering disabling Twitch command execution (`/`, `.`), redacting hyperlinks, and censoring blocked terms.
  - Token-bucket rate limiting across global broadcast intervals and per-viewer cooldowns.
- **Diagnostics HTTP Gateway**:
  - Lightweight `/healthz` and `/status` endpoints for health probes, metrics scrapers, and orchestration systems.

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
|   1. Self-message filter and empty payload rejection                           |
|   2. Trigger prefix detection (!bot, @BotName, @BotLogin)                      |
|        +-- NO TRIGGER --> Ambient Room Buffer (internal/room)                  |
|        `-- TRIGGERED                                                           |
|             +-- Admin Command (Broadcaster/Moderator) -> Deterministic Handler |
|             `-- Conversational Pipeline:                                       |
|                  +-- Rate Limiter Check (internal/ratelimit)                   |
|                  +-- Safety Guardrail Inspection (internal/safety)             |
|                  +-- Profile & Social Slots Load (internal/store)              |
|                  +-- Context Cache Prompt Construction (internal/llm)          |
|                  +-- Upstream LLM Inference (DeepSeek)                         |
|                  +-- Social Transition State Machine (internal/social)         |
|                  +-- Atomic Disk Flush (internal/store)                        |
|                  `-- Threaded Chat Reply (internal/twitch)                     |
+--------------------------------------------------------------------------------+
        |                                                                |
        v                                                                v
+-----------------------+                                      +-----------------+
| HTTP Gateway          |                                      | YAML Persona    |
| (/healthz, /status)   |                                      | Definitions     |
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

### Prerequisites
- **Go 1.22+** (compiled and tested with Go 1.24).
- A valid **DeepSeek API Key** ([DeepSeek Platform](https://platform.deepseek.com/)).
- A registered **Twitch Application** on the [Twitch Developer Console](https://dev.twitch.tv/console) to obtain a `Client ID`.

### 1. Installation
```bash
git clone https://github.com/RAgredaIpar/twitch-social-bot.git
cd twitch-social-bot
cp .env.example .env
```

Configure `.env` with your deployment values:
```ini
LLM_API_KEY=sk-your-deepseek-api-key
TWITCH_CLIENT_ID=your_twitch_client_id
TWITCH_BROADCASTER_LOGIN=channel_broadcaster_username
TWITCH_BOT_LOGIN=bot_account_username
```

### 2. Execution
```bash
go run ./cmd/bot
```

On initial launch without pre-existing tokens, the terminal initiates the OAuth 2.0 Device Code Flow:
```text
=================================================================
TWITCH AUTHORIZATION REQUIRED
Visit: https://www.twitch.tv/activate
Code: WXYZ-1234
=================================================================
```
Open the URL in a browser authenticated as the bot user, enter the verification code, and grant authorization. Refresh tokens are automatically written to `.tokens.json` (git-ignored) and renewed periodically.

---

## Container Deployment

A multi-stage Docker build is available producing a minimal, rootless static binary:

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
| `LLM_API_KEY` | String | *(Required)* | DeepSeek API authentication key. |
| `LLM_BASE_URL` | String | `https://api.deepseek.com` | Base endpoint URL for LLM completions. |
| `LLM_MODEL` | String | `deepseek-flash` | Identifier of model utilized for conversational generation. |
| `LLM_TIMEOUT_MS` | Integer | `8000` | Upstream LLM request timeout in milliseconds. |
| `TWITCH_CLIENT_ID` | String | `""` | Registered Twitch Developer Application Client ID. |
| `TWITCH_BROADCASTER_LOGIN`| String | `""` | Twitch username of the broadcaster channel. |
| `TWITCH_BOT_LOGIN` | String | `""` | Twitch username representing the chat bot account. |
| `TWITCH_ACCESS_TOKEN` | String | `""` | Initial OAuth access token override (optional). |
| `TWITCH_REFRESH_TOKEN` | String | `""` | Initial OAuth refresh token override (optional). |
| `TWITCH_TOKEN_FILE` | String | `.tokens.json` | Local filepath for encrypted persistence of OAuth tokens. |
| `PERSONA_DIR` | String | `personas` | Directory containing declarative YAML persona definitions. |
| `PERSONA_FILE` | String | `personas/cohost_amigos.yaml` | Initial YAML persona loaded at startup. |
| `STORE_PATH` | String | `data/social.db` | Local filesystem path for the atomic JSON social database. |
| `GLOBAL_COOLDOWN_MS` | Integer | `3000` | Global broadcast cooldown between bot messages in milliseconds. |
| `USER_COOLDOWN_MS` | Integer | `15000` | Per-viewer rate limit cooldown in milliseconds. |
| `ROOM_BUFFER_SIZE` | Integer | `25` | Maximum sliding window capacity of ambient chat messages retained. |
| `MAX_VIEWER_FACTS` | Integer | `15` | Default maximum quantity of episodic facts stored per viewer. |
| `ROOM_BUFFER_MAX_AGE_SEC` | Integer | `600` | Maximum age in seconds before ambient chat messages expire. |
| `HTTP_ADDR` | String | `127.0.0.1:8080` | Network address and port for the diagnostics HTTP gateway. |

---

## Diagnostic Endpoints

The internal HTTP server exposes monitoring routes:

### `GET /healthz`
Healthcheck probe for orchestrators and container engines:
```json
{
  "status": "ok"
}
```

### `GET /status`
Detailed operational runtime state:
```json
{
  "paused": false,
  "active_persona_id": "cohost_amigos",
  "active_persona_name": "Luna",
  "bot_name": "LunaBot",
  "trigger_prefix": "!bot",
  "active_slots": {
    "bff": [
      {
        "slot_id": "bff",
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

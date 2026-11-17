![Twitch Social Bot](docs/assets/banner.webp)

# Twitch Social Bot

Agente conversacional de chat con memoria social persistente, afinidad dinámica de espectadores, ritmo humano de conversación y personalidades YAML intercambiables en caliente, diseñado para transmisiones interactivas en vivo de Twitch.

Desarrollado en **Go puro** (100% librería estándar, cero dependencias externas, cero CGO), compatible con **DeepSeek, Claude (Anthropic), ChatGPT (OpenAI), Google Gemini, Groq, Grok (xAI) y APIs Custom/Privadas**, y conectado directamente a Twitch EventSub mediante WebSocket.

[English](README.md) | Español

---

## Características

- **Librería Estándar de Go Pura**: Cero dependencias externas en `go.mod`. Todo el sistema (servidor HTTP, cliente WebSocket, parser YAML, conversor de imágenes a Braille ANSI, auto-actualizador y flujo OAuth) utiliza exclusivamente la librería estándar de Go.
- **CLI Moderna Interactiva (`↑`/`↓` + `Enter`)**:
  - **Tutorial Guiado de Primera Vez**: Guía paso a paso al usuario nuevo para elegir idioma, proveedor de IA + API Key, aplicación en Twitch Developer Console, cuenta del bot y personalidad inicial.
  - **Avatares Braille y Color Temático por Personalidad**: Muestra un avatar en Unicode Braille (`28×12`, resolución `56×48`) y cambia el color del marco de la CLI según la personalidad activa (`Mochi` Esmeralda, `Celeste` Rosa, `Lyra` Cian, `Dante` Rojo).
  - **Creador Integrado de Personalidades**: Permite crear nuevas personalidades desde la CLI (`+ Crear Nueva Personalidad`), incluso arrastrando y soltando cualquier imagen `.png`/`.jpg`/`.gif` para convertirla a Braille al instante.
- **Soporte Multi-IA y Autodescubrimiento de Modelos**:
  - Compatible con **DeepSeek**, **Claude (Anthropic)** (`/v1/messages`), **ChatGPT (OpenAI)**, **Google Gemini**, **Groq**, **Grok (xAI)** y **Custom / API Privada** (OpenRouter, Ollama, vLLM, etc.).
  - Consulta en vivo `GET /models` con tu API Key para elegir automáticamente el modelo de chat más reciente sin depender de listas fijas, protegido por un Circuit Breaker de 3 estados (`Closed`, `Open`, `Half-Open`).
- **Multi-Idioma Extensible (`locales/*.yaml`)**:
  - Intercambiable entre **Inglés (`en`)** y **Español (`es`)** (y extensible con archivos YAML propios en `locales/`), traduciendo prompts del LLM, respuestas del bot y toda la interfaz CLI.
- **Comportamiento Humano en el Chat**:
  - **Ritmo Variable y Reacciones Mínimas**: Ante mensajes muy cortos (`"hola"`, `"xd"`, `"f"`), responde igual de breve (1–4 palabras) y varía la extensión de forma natural en cada turno.
  - **Anti-Repetición y Postura Firme**: Aplica `frequency_penalty`/`presence_penalty`, prohíbe muletillas de asistente de IA y exige tomar postura directa sin respuestas tibias.
  - **Estado de Ánimo ("Mood") Dinámico**: Cambia entre `chill`, `hype`, `annoyed` (picado/cortante) y `playful` según las interacciones recientes, relajándose tras 10 minutos.
  - **Jerarquía de Twitch y Contexto del Stream**: Detecta badges (`Broadcaster`, `Moderador`, `VIP`, `Suscriptor`, `Viewer nuevo`) y consulta periódicamente el juego/categoría y título del directo vía Twitch Helix.
- **Motor Social Persistente y Puestos Competitivos**:
  - Afinidad individual (`-100` a `+100`), tiers dinámicos, títulos ganados, memoria episódica de hechos (por defecto 15) y puestos honoríficos competitivos con robo por mayor afinidad.
- **Personalidades YAML Intercambiables en Caliente**:
  - Catálogo detallado y guía para autores: [Catálogo de Personalidades (Español)](docs/personas.es.md) | [Persona Catalog (English)](docs/personas.md).
  - Personalidades incluidas: **Mochi** (`cohost_amigos.yaml`), **Celeste** (`drama_romance.yaml`), **Lyra** (`rpg_facciones.yaml`) y **Dante** (`clan_jerarquia.yaml`).
- **Actualizador Automático Obligatorio (Git y GitHub Releases)**:
  - Comprueba nuevas versiones al iniciar y solicita actualizar y reiniciar automáticamente conservando intactos `.env`, `.tokens.json` y `data/`.
- **Transporte Nativo de Twitch, Seguridad y Diagnóstico**:
  - EventSub WebSocket (`channel.chat.message`), OAuth 2.0 Device Code Flow (`twitch.tv/activate`), filtros anti-inyección y de salida, control de cooldowns y endpoints `/healthz` y `/status`.

---

## Arquitectura

```
                      +----------------------------------+
                      |         Sala de Chat Twitch      |
                      +-----------------+----------------+
                                        |
                             Trama EventSub WebSocket
                                        v
+--------------------------------------------------------------------------------+
| Orquestador (internal/engine)                                                  |
|                                                                                |
|   1. Filtro de mensajes propios y lectura de badges (Broadcaster/Mod/VIP/Sub)  |
|   2. Detección de prefijo (!bot, @NombreBot, @LoginBot)                        |
|        +-- SIN ACTIVADOR --> Buffer Ambiental de Sala (internal/room)          |
|        `-- ACTIVADO                                                            |
|             +-- Comando Admin (Broadcaster/Moderador) -> Manejo Determinista   |
|             `-- Pipeline Conversacional:                                       |
|                  +-- Control de Cooldowns (internal/ratelimit)                 |
|                  +-- Inspección de Guardrails (internal/safety)                |
|                  +-- Perfil, Mood y Contexto en Vivo de Twitch Helix           |
|                  +-- Prompt Localizado + Ritmo Variable (internal/llm)         |
|                  +-- Inferencia Multi-IA (OpenAI-compat / Anthropic)           |
|                  +-- Máquina de Estados Sociales (internal/social)             |
|                  +-- Persistencia Atómica en Disco (internal/store)            |
|                  `-- Respuesta en Hilo de Chat (internal/twitch)               |
+--------------------------------------------------------------------------------+
        |                                                                |
        v                                                                v
+-----------------------+                                      +-----------------+
| Pasarela HTTP         |                                      | Personalidades  |
| (/healthz, /status)   |                                      | YAML y Locales  |
+-----------------------+                                      +-----------------+
```

---

## Comandos de Chat

### Comandos Públicos
- `!bot <mensaje>` o `@NombreBot <mensaje>`: Interactúa con la personalidad activa. Evalúa variaciones de afinidad, registra hechos episódicos y consume tiempo de cooldown del usuario.

### Comandos de Moderación y Administración
Restringidos al Broadcaster (dueño del canal) y Moderadores. Ignoran los cooldowns conversacionales y se ejecutan de forma determinista:

| Comando | Argumentos | Descripción |
|---|---|---|
| `!bot pause` | Ninguno | Suspende temporalmente las respuestas conversacionales. |
| `!bot resume` | Ninguno | Reanuda el procesamiento conversacional normal. |
| `!bot status` | Ninguno | Informa en el chat la personalidad activa, estado operativo y titulares de puestos. |
| `!bot persona` | `<persona_id>` | Cambia en caliente la personalidad activa (ej. `!bot persona rpg_facciones`). |
| `!bot set` | `@usuario <afinidad>` | Sobrescribe la afinidad de un espectador (`-100` a `+100`), recalculando rango y puestos. |
| `!bot reset` | `@usuario` | Purga hechos episódicos, reinicia la afinidad a cero y limpia cooldowns del usuario. |

---

## Inicio Rápido

### Opción A: Release Listo para Windows (Sin instalar Go ni Git)
1. Descarga `twitch-social-bot-windows.zip` desde [GitHub Releases](https://github.com/RAgredaIpar/twitch-social-bot/releases).
2. Descomprime la carpeta y haz doble clic en **`twitch-social-bot.exe`**.
3. Sigue el tutorial guiado inicial usando las flechas (`↑`/`↓` + `Enter`).

### Opción B: Desde Código Fuente (Desarrolladores)
- **Prerrequisitos**: **Go 1.22+** y una API Key de tu proveedor de IA favorito (más un Client ID opcional de [dev.twitch.tv/console](https://dev.twitch.tv/console)).
```bash
git clone https://github.com/RAgredaIpar/twitch-social-bot.git
cd twitch-social-bot
go run ./cmd/bot
```
- **Flags disponibles**:
  - `--run`: Omite el menú CLI interactivo y arranca el bot directamente (usado en Docker/servidores).
  - `--no-update`: Omite la comprobación de actualizaciones al iniciar.
  - `--env <ruta>`: Carga o guarda un archivo `.env` específico.

Al iniciar por primera vez con Twitch sin tokens guardados, la consola inicia el flujo OAuth 2.0 Device Code:
```text
=================================================================
TWITCH AUTHORIZATION REQUIRED
Visit: https://www.twitch.tv/activate
Code: WXYZ-1234
=================================================================
```
Abre `https://www.twitch.tv/activate` **con la sesión iniciada en la cuenta del bot**, ingresa el código y acepta los permisos. Los tokens se guardan en `.tokens.json` (ignorado por Git) y se renuevan automáticamente.

---

## Despliegue en Contenedor

Disponible compilación Docker multi-etapa para un binario estático sin privilegios de root (arranca directo con `--run`):

```bash
# Construir imagen
make docker-build

# Ejecutar contenedor con volumen persistente
docker run -d \
  --name twitch-social-bot \
  --restart unless-stopped \
  --env-file .env \
  -p 8080:8080 \
  -v $(pwd)/data:/app/data \
  twitch-social-bot:latest
```

---

## Comandos Makefile

```bash
make test          # Ejecuta los tests con detector de condiciones de carrera (-race)
make test-coverage # Genera perfil de ejecución y reporte de cobertura
make build         # Compila el binario estático ejecutable en bin/bot
make run           # Ejecuta la aplicación localmente
make clean         # Limpia binarios generados y artefactos de prueba
make fmt           # Ejecuta gofmt en todos los paquetes del repositorio
```

---

## Referencia de Configuración

| Variable | Tipo | Por Defecto | Descripción |
|---|---|---|---|
| `BOT_LANGUAGE` | String | `en` | Idioma activo (`en`, `es`, o archivo propio en `locales/`). |
| `LOCALE_DIR` | String | `locales` | Directorio con los paquetes de idioma YAML. |
| `LLM_PROVIDER` | String | `deepseek` | Proveedor de IA (`deepseek`, `claude`, `openai`, `gemini`, `groq`, `xai`, `custom`). |
| `LLM_API_KEY` | String | *(Requerido)* | Clave de autenticación del proveedor de IA elegido. |
| `LLM_BASE_URL` | String | `https://api.deepseek.com` | URL base del servicio de inferencia. |
| `LLM_MODEL` | String | `deepseek-chat` | Identificador del modelo (autodescubierto vía `GET /models` en la CLI). |
| `LLM_TIMEOUT_MS` | Integer | `8000` | Tiempo de espera máximo hacia el LLM en milisegundos. |
| `TWITCH_CLIENT_ID` | String | `""` | Client ID de la aplicación registrada en Twitch Developers. |
| `TWITCH_BROADCASTER_LOGIN`| String | `""` | Usuario de Twitch dueño del canal (streamer). |
| `TWITCH_BOT_LOGIN` | String | `""` | Usuario de Twitch de la cuenta que actuará como bot. |
| `TWITCH_ACCESS_TOKEN` | String | `""` | Token OAuth inicial opcional. |
| `TWITCH_REFRESH_TOKEN` | String | `""` | Token de refresco OAuth inicial opcional. |
| `TWITCH_TOKEN_FILE` | String | `.tokens.json` | Ruta del archivo local donde se persisten los tokens OAuth. |
| `PERSONA_DIR` | String | `personas` | Directorio con las definiciones declarativas de personalidades YAML. |
| `PERSONA_FILE` | String | `personas/cohost_amigos.yaml` | Personalidad cargada por defecto al arrancar. |
| `STORE_PATH` | String | `data/social.db` | Ruta de la base de datos social JSON atómica. |
| `GLOBAL_COOLDOWN_MS` | Integer | `3000` | Tiempo mínimo entre respuestas globales del bot en milisegundos. |
| `USER_COOLDOWN_MS` | Integer | `15000` | Tiempo de espera por espectador en milisegundos. |
| `ROOM_BUFFER_SIZE` | Integer | `25` | Capacidad de ventana deslizante de mensajes ambientales recientes. |
| `MAX_VIEWER_FACTS` | Integer | `15` | Cantidad máxima por defecto de hechos episódicos guardados por espectador. |
| `ROOM_BUFFER_MAX_AGE_SEC` | Integer | `600` | Antigüedad máxima en segundos antes de expirar mensajes ambientales. |
| `HTTP_ADDR` | String | `127.0.0.1:8080` | Dirección IP y puerto para la pasarela HTTP de diagnóstico. |

---

## Endpoints de Diagnóstico

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

## Licencia

Este proyecto está licenciado bajo los términos de la [Licencia MIT](LICENSE).

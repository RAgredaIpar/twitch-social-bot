# 🤖 Twitch Social Bot

> **Bot conversacional con memoria social persistente, afinidad dinámica y personalidades intercambiables en tiempo real para transmisiones en directo de Twitch.**

Desarrollado en **Go puro (100% biblioteca estándar, cero dependencias externas, cero CGO)**, impulsado por modelos **DeepSeek** con **Context Caching** y conectado de forma nativa a **Twitch EventSub WebSocket**.

---

## 🌟 Características Principales

- **100% Go Estándar**: Sin dependencias de terceros en `go.mod`. Todo el stack (servidor HTTP, cliente WebSocket, analizador YAML/JSON, limitador de tasa y cliente OAuth) está implementado utilizando exclusivamente la biblioteca estándar de Go.
- **Memoria Social Persistente**:
  - Escala de afinidad individual de `-100` a `+100` para cada espectador.
  - Niveles de relación (*Tiers*) dinámicos que alteran el comportamiento y tono del bot.
  - Títulos honoríficos asignados por la IA (*Earned Titles*).
  - Memoria episódica de hechos aprendidos sobre cada usuario (hasta 5 hechos con deduplicación y política FIFO).
  - Historial de turnos recientes de conversación por espectador (hasta 4 turnos con política FIFO).
- **Puestos Sociales Exclusivos (*Social Slots*)**:
  - Títulos únicos con cupos limitados (ej. *Mejor Amigo*, *Archienemigo*, *Mano Derecha*).
  - Reglas de asignación automática según afinidad mínima/máxima.
  - Desafíos de usurpación (*Outrank Steal*): los espectadores con mayor afinidad pueden arrebatar puestos ocupados.
- **Motor de Personalidades YAML con Recarga en Caliente (*Hot-Reloading*)**:
  - Define personalidades ricas mediante archivos YAML legibles en la carpeta `personas/`.
  - Cambia de personalidad en caliente mediante el comando `!bot persona <id>` en el chat sin reiniciar el proceso.
  - Incluye 4 arquetipos listos para producción:
    - `cohost_amigos.yaml`: Luna, una co-host amigable, cálida y bromista.
    - `rpg_facciones.yaml`: Guardián de Facciones con lealtad y rangos de gremio.
    - `clan_jerarquia.yaml`: Jerarquía de clan tribal con combate por respeto.
    - `drama_romance.yaml`: Telenovela interactiva cargada de drama y emociones.
- **DeepSeek con Context Caching & Circuit Breaker**:
  - Prompt del sistema determinista optimizado para el mecanismo de **Context Caching** de DeepSeek, reduciendo significativamente los costos y la latencia.
  - Disyuntor (*Circuit Breaker*) de tres estados (`Closed`, `Open`, `Half-Open`) para resiliencia ante caídas del proveedor de IA.
- **Conectividad Nativa con Twitch**:
  - Suscripción en tiempo real a `channel.chat.message` mediante **Twitch EventSub WebSocket**.
  - Flujo de autorización interactivo en terminal mediante **OAuth 2.0 Device Code Flow** (sin necesidad de configurar servidores de callback locales).
  - Envío de respuestas hiladas a través de **Twitch Helix API**.
- **Seguridad y Moderación**:
  - Protección contra inyecciones de prompt y caracteres de control.
  - Sanitización estricta de salidas para evitar ejecución de comandos de Twitch (`/ban`, `.timeout`, etc.).
  - Reemplazo automático de enlaces externos y filtrado de palabras bloqueadas.
  - Cooldowns configurables: global (ej. 3s) y por usuario (ej. 15s) para evitar spam.
- **Pasarela HTTP de Diagnóstico**:
  - Endpoints ligeros `/healthz` y `/status` para monitorización, dashboards y orquestadores (Docker, Kubernetes).

---

## 🏛️ Arquitectura del Sistema

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
|   1. Filtro de mensajes propios y vacíos                                       |
|   2. Detección de disparador (!bot, @BotName, @BotLogin)                       |
|        ├── NO Disparado ──> Buffer de Sala (internal/room) [Contexto Ambiental]|
|        └── DISPARADO                                                           |
|             ├── Comando Admin (Broadcaster / Mod) ──> Ejecución Determinista   |
|             └── Mensaje Conversacional:                                        |
|                  ├── Limitador de Cooldowns (internal/ratelimit)               |
|                  ├── Filtro de Seguridad (internal/safety)                     |
|                  ├── Carga de Perfil & Puestos (internal/store)                |
|                  ├── Prompt Caching + Inferencia (internal/llm)                |
|                  ├── Transición Social Determinista (internal/social)          |
|                  ├── Persistencia Atómica (internal/store)                     |
|                  └── Respuesta a Twitch Chat (internal/twitch)                 |
+--------------------------------------------------------------------------------+
        |                                                                |
        v                                                                v
+-----------------------+                                      +-----------------+
| Pasarela HTTP         |                                      | Archivos YAML   |
| (/healthz, /status)   |                                      | de Personalidad |
+-----------------------+                                      +-----------------+
```

---

## 💬 Comandos en el Chat de Twitch

### Para Todos los Espectadores
- `!bot <mensaje>` o `@LunaBot <mensaje>`: Envía una consulta o interactúa con el bot. Consume el cooldown por usuario y evalúa la afinidad según la conversación.

### Comandos Administrativos (Solo Broadcaster y Moderadores)
Bypasean los cooldowns y se ejecutan de manera determinista:

| Comando | Descripción |
|---|---|
| `!bot pause` | Pausa temporalmente las respuestas conversacionales del bot. |
| `!bot resume` | Reanuda el procesamiento de mensajes conversacionales. |
| `!bot status` | Muestra en el chat el estado actual, personalidad activa y titulares de puestos sociales. |
| `!bot persona <id>` | Cambia y recarga en caliente la personalidad activa (ej. `!bot persona rpg_facciones`). |
| `!bot set @usuario <afinidad>` | Asigna manualmente un puntaje de afinidad (`-100` a `100`), actualizando su nivel y puestos sociales. |
| `!bot reset @usuario` | Restablece por completo la memoria social y el cooldown del espectador indicado. |

---

## 🚀 Guía de Inicio Rápido

### Requisitos Previos
- **Go 1.22+** instalado (desarrollado y probado con Go 1.24).
- Una clave de API de **DeepSeek** ([DeepSeek Platform](https://platform.deepseek.com/)).
- Una cuenta y aplicación de **Twitch** registrada en la [Consola de Desarrolladores de Twitch](https://dev.twitch.tv/console) para obtener tu `Client ID`.

### 1. Clonar y Configurar
```bash
git clone https://github.com/twitch-social-bot/twitch-social-bot.git
cd twitch-social-bot
cp .env.example .env
```

Edita `.env` con tus credenciales:
```ini
LLM_API_KEY=sk-tu-api-key-de-deepseek
TWITCH_CLIENT_ID=tu_twitch_client_id
TWITCH_BROADCASTER_LOGIN=nombre_del_streamer
TWITCH_BOT_LOGIN=nombre_del_bot
```

### 2. Ejecutar con Go
```bash
go run ./cmd/bot
```

> **Primera Ejecución**: Si no proporcionas `TWITCH_ACCESS_TOKEN`, la consola mostrará un enlace y un código de verificación:
> ```
> =================================================================
> ¡AUTORIZACIÓN DE TWITCH REQUERIDA!
> Visita: https://www.twitch.tv/activate
> Código: WXYZ-1234
> =================================================================
> ```
> Abre la URL en tu navegador, inicia sesión con la cuenta del bot y autoriza la aplicación. El bot guardará los tokens en `.tokens.json` y los renovará automáticamente.

---

## 🐳 Despliegue con Docker

El proyecto incluye un `Dockerfile` multi-etapa que compila un binario estático ligero en Alpine Linux:

### Compilación y Ejecución
```bash
# Construir la imagen
make docker-build

# Ejecutar el contenedor con variables de entorno y volumen persistente
make docker-run
```

O directamente con Docker CLI:
```bash
docker run -d \
  --name twitch-social-bot \
  --restart unless-stopped \
  --env-file .env \
  -p 8080:8080 \
  -v $(pwd)/data:/app/data \
  twitch-social-bot:latest
```

---

## 🛠️ Comandos de Desarrollo (`Makefile`)

```bash
make test          # Ejecuta la suite de pruebas completa con detector de carreras (-race)
make test-coverage # Ejecuta pruebas y genera reporte de cobertura de código
make build         # Compila el binario ejecutable en bin/bot
make run           # Ejecuta el bot en entorno local
make clean         # Limpia binarios y archivos de cobertura temporales
make fmt           # Aplica formato estándar a todos los archivos Go
```

---

## ⚙️ Variables de Entorno

| Variable | Tipo | Por Defecto | Descripción |
|---|---|---|---|
| `LLM_API_KEY` | String | *(Requerido)* | Clave de autenticación para la API de DeepSeek. |
| `LLM_BASE_URL` | String | `https://api.deepseek.com` | Endpoint base del proveedor de LLM. |
| `LLM_MODEL` | String | `deepseek-flash` | Modelo de lenguaje a utilizar. |
| `LLM_TIMEOUT_MS` | Entero | `8000` | Tiempo de espera máximo para peticiones LLM en milisegundos. |
| `TWITCH_CLIENT_ID` | String | `""` | Client ID de la aplicación en Twitch Developer Console. |
| `TWITCH_BROADCASTER_LOGIN`| String | `""` | Nombre de usuario del canal emisor. |
| `TWITCH_BOT_LOGIN` | String | `""` | Nombre de usuario de la cuenta del bot. |
| `TWITCH_ACCESS_TOKEN` | String | `""` | Token de acceso OAuth inicial (opcional). |
| `TWITCH_REFRESH_TOKEN` | String | `""` | Token de actualización OAuth inicial (opcional). |
| `TWITCH_TOKEN_FILE` | String | `.tokens.json` | Ruta local donde se almacenan los tokens OAuth cifrados. |
| `PERSONA_DIR` | String | `personas` | Directorio de definición de personalidades YAML. |
| `PERSONA_FILE` | String | `personas/cohost_amigos.yaml` | Personalidad cargada por defecto al arrancar. |
| `STORE_PATH` | String | `data/social.db` | Ruta del archivo de persistencia atómica en disco. |
| `GLOBAL_COOLDOWN_MS` | Entero | `3000` | Cooldown global entre mensajes en milisegundos. |
| `USER_COOLDOWN_MS` | Entero | `15000` | Cooldown por espectador individual en milisegundos. |
| `ROOM_BUFFER_SIZE` | Entero | `25` | Cantidad de mensajes ambientales recientes en memoria. |
| `ROOM_BUFFER_MAX_AGE_SEC` | Entero | `600` | Antigüedad máxima de los mensajes en el buffer (segundos). |
| `HTTP_ADDR` | String | `127.0.0.1:8080` | Dirección y puerto para la pasarela HTTP de diagnóstico. |

---

## 📊 Endpoints de Diagnóstico HTTP

El servidor expone dos endpoints REST ligeros en formato JSON:

### `GET /healthz`
Comprobación de operatividad para Docker Healthcheck o sondas de Kubernetes.
```json
{
  "status": "ok"
}
```

### `GET /status`
Instantánea en tiempo real del estado interno del motor:
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
        "display_name": "ViewerFiel",
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

## 📄 Licencia

Este proyecto se distribuye bajo la licencia **MIT**. Consulta el archivo [LICENSE](LICENSE) para más detalles.

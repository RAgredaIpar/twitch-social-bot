# Twitch Social Bot

Agente conversacional de chat con memoria social persistente, afinidad dinámica de espectadores y personalidades YAML intercambiables en caliente, diseñado para transmisiones interactivas en vivo de Twitch.

Desarrollado en Go puro (100% biblioteca estándar, cero dependencias externas, cero CGO), impulsado por modelos DeepSeek con optimizaciones de almacenamiento en caché de contexto y conectado directamente a Twitch EventSub a través de WebSocket.

[English](README.md) | Español

---

## Características Principales

- **Biblioteca Estándar de Go al 100%**: Sin dependencias de terceros en `go.mod`. Todo el ecosistema de ejecución (servidor HTTP, cliente WebSocket, analizador YAML, serializador JSON, limitadores de tasa y flujo OAuth) utiliza exclusivamente la biblioteca estándar de Go.
- **Motor de Relaciones Sociales Persistentes**:
  - Seguimiento individual del puntaje de afinidad acotado entre `-100` y `+100` por espectador.
  - Niveles de relación dinámicos (*tiers*) que alteran el tono, familiaridad y límites de decisión.
  - Títulos honoríficos otorgados dinámicamente según el contexto conversacional acumulado.
  - Memoria episódica que rastrea datos sobre cada espectador (predeterminado configurable: 15 hechos, política FIFO y deduplicación).
  - Memoria conversacional de múltiples turnos que retiene intercambios de diálogo recientes por participante.
- **Puestos Sociales Competitivos (*Social Slots*)**:
  - Puestos honoríficos de capacidad limitada (ej. *Mejor Amigo*, *Archienemigo*, *Mano Derecha*).
  - Calificación determinista y revocación automática basada en umbrales de afinidad.
  - Mecánica de usurpación de puestos (*outrank stealing*) que permite a espectadores con mayor afinidad desplazar a los ocupantes actuales.
- **Personalidades YAML Intercambiables en Caliente**:
  - Archivos de configuración declarativos ubicados en el directorio `personas/`.
  - Catálogo detallado y guía para autores: [docs/personas.es.md](docs/personas.es.md) ([English](docs/personas.md)).
  - Cambio en tiempo de ejecución mediante comandos de chat (`!bot persona <id>`) sin reiniciar el proceso.
  - Plantillas de producción incluidas:
    - `cohost_amigos.yaml`: Co-host de transmisión cálida y divertida.
    - `rpg_facciones.yaml`: Guardián de facción con rangos de gremio y lealtad.
    - `clan_jerarquia.yaml`: Jerarquía de clan tribal con desafíos basados en respeto.
    - `drama_romance.yaml`: Narrativa interactiva con drama y tensiones emocionales.
- **Integración con DeepSeek (Context Caching y Circuit Breaker)**:
  - Mensajes de sistema deterministas estructurados para el caché de contexto de DeepSeek, reduciendo latencia y consumo de tokens.
  - Disyuntor de tres estados (`Closed`, `Open`, `Half-Open`) para aislamiento de fallos ante caídas del proveedor upstream.
- **Transporte Nativo para Twitch**:
  - Comunicación bidireccional de baja latencia mediante EventSub WebSocket (`channel.chat.message`).
  - Flujo de autorización interactivo en terminal mediante OAuth 2.0 Device Code Flow (RFC 8628), sin requerir túneles webhooks públicos ni servidores de callback locales.
  - Respuestas en hilo enviadas a través de la API Helix de Twitch.
- **Seguridad, Moderación y Control de Tasa**:
  - Sanitización estricta de entradas para prevenir inyecciones de comandos y fuga de prompts.
  - Filtrado de salidas que bloquea la ejecución de comandos de Twitch (`/`, `.`), redacta enlaces externos y censura términos prohibidos.
  - Limitación de tasa mediante cubos de tokens para intervalos de emisión global y enfriamiento individual por espectador.
- **Pasarela HTTP de Diagnóstico**:
  - Endpoints ligeros `/healthz` y `/status` para sondeos de salud, recolección de métricas y sistemas de orquestación.

---

## Arquitectura

```
                      +----------------------------------+
                      |        Sala de Chat Twitch       |
                      +-----------------+----------------+
                                        |
                             Trama EventSub WebSocket
                                        v
+--------------------------------------------------------------------------------+
| Orquestador (internal/engine)                                                  |
|                                                                                |
|   1. Filtro de mensajes propios y rechazo de carga útil vacía                  |
|   2. Detección de prefijo disparador (!bot, @BotName, @BotLogin)               |
|        +-- NO DISPARADO --> Búfer de Sala Ambiental (internal/room)            |
|        `-- DISPARADO                                                           |
|             +-- Comando Admin (Emisor/Moderador) -> Controlador Determinista   |
|             `-- Flujo Conversacional:                                          |
|                  +-- Verificación de Cooldowns (internal/ratelimit)            |
|                  +-- Inspección de Filtros de Seguridad (internal/safety)      |
|                  +-- Carga de Perfil y Puestos Sociales (internal/store)       |
|                  +-- Construcción de Prompt con Context Cache (internal/llm)   |
|                  +-- Inferencia Upstream LLM (DeepSeek)                        |
|                  +-- Máquina de Estados de Transición Social (internal/social) |
|                  +-- Volcado Atómico a Disco (internal/store)                  |
|                  `-- Envío de Respuesta a Chat (internal/twitch)               |
+--------------------------------------------------------------------------------+
        |                                                                |
        v                                                                v
+-----------------------+                                      +-----------------+
| Pasarela HTTP         |                                      | Definiciones    |
| (/healthz, /status)   |                                      | YAML de Persona |
+-----------------------+                                      +-----------------+
```

---

## Comandos en el Chat

### Comandos Públicos
- `!bot <mensaje>` o `@NombreBot <mensaje>`: Interactúa con la personalidad activa. Evalúa variaciones de afinidad, registra hechos episódicos y consume tiempo de cooldown del usuario.

### Comandos de Moderación y Administración
Restringidos al emisor del canal y moderadores. Omiten los límites de tasa conversacionales y se ejecutan de manera determinista:

| Comando | Argumentos | Descripción |
|---|---|---|
| `!bot pause` | Ninguno | Suspende temporalmente las respuestas conversacionales. |
| `!bot resume` | Ninguno | Reanuda el procesamiento conversacional habitual. |
| `!bot status` | Ninguno | Informa en el chat la personalidad activa, estado operativo y titulares de puestos. |
| `!bot persona` | `<persona_id>` | Cambia en caliente la personalidad activa (ej. `!bot persona rpg_facciones`). |
| `!bot set` | `@usuario <afinidad>` | Sobrescribe el puntaje de afinidad (`-100` a `+100`), recalculando nivel y puestos. |
| `!bot reset` | `@usuario` | Borra la memoria episódica, restablece la afinidad inicial y limpia cooldowns del usuario. |

---

## Inicio Rápido

### Requisitos Previos
- **Go 1.22+** (compilado y probado con Go 1.24).
- Una **clave de API de DeepSeek** ([DeepSeek Platform](https://platform.deepseek.com/)).
- Una **aplicación registrada en Twitch** en la [Consola de Desarrolladores de Twitch](https://dev.twitch.tv/console) para obtener un `Client ID`.

### 1. Instalación
```bash
git clone https://github.com/RAgredaIpar/twitch-social-bot.git
cd twitch-social-bot
cp .env.example .env
```

Configura `.env` con los valores de despliegue:
```ini
LLM_API_KEY=sk-tu-clave-de-deepseek
TWITCH_CLIENT_ID=tu_twitch_client_id
TWITCH_BROADCASTER_LOGIN=nombre_usuario_canal
TWITCH_BOT_LOGIN=nombre_usuario_bot
```

### 2. Ejecución
```bash
go run ./cmd/bot
```

En el primer inicio sin tokens previos, la terminal inicia el flujo interactivo de código de dispositivo (Device Code Flow):
```text
=================================================================
¡AUTORIZACIÓN DE TWITCH REQUERIDA!
Visita: https://www.twitch.tv/activate
Código: WXYZ-1234
=================================================================
```
Abre la URL en el navegador con la sesión iniciada en la cuenta del bot, introduce el código de verificación y autoriza la aplicación. Los tokens se almacenan en `.tokens.json` (ignorado por Git) y se renuevan periódicamente de forma automática.

---

## Despliegue en Contenedores (Docker)

El proyecto incluye un `Dockerfile` multi-etapa que produce un binario estático optimizado y sin privilegios de root:

```bash
# Construir la imagen del contenedor
make docker-build

# Ejecutar el contenedor con persistencia de datos
docker run -d \
  --name twitch-social-bot \
  --restart unless-stopped \
  --env-file .env \
  -p 8080:8080 \
  -v $(pwd)/data:/app/data \
  twitch-social-bot:latest
```

---

## Comandos de Desarrollo (Makefile)

```bash
make test          # Ejecuta las pruebas con el detector de carreras activado (-race)
make test-coverage # Genera perfil de ejecución de pruebas y reporte de cobertura
make build         # Compila el binario ejecutable estático en bin/bot
make run           # Ejecuta la aplicación de forma local
make clean         # Limpia binarios generados, artefactos temporales y perfiles de pruebas
make fmt           # Aplica gofmt en todos los paquetes del repositorio
```

---

## Referencia de Configuración

| Variable | Tipo | Por Defecto | Descripción |
|---|---|---|---|
| `LLM_API_KEY` | String | *(Requerido)* | Clave de autenticación de la API de DeepSeek. |
| `LLM_BASE_URL` | String | `https://api.deepseek.com` | URL base del endpoint para completado de LLM. |
| `LLM_MODEL` | String | `deepseek-flash` | Modelo utilizado para la generación conversacional. |
| `LLM_TIMEOUT_MS` | Entero | `8000` | Tiempo de espera límite para solicitudes al LLM en milisegundos. |
| `TWITCH_CLIENT_ID` | String | `""` | Client ID de la aplicación registrada en Twitch Developers. |
| `TWITCH_BROADCASTER_LOGIN`| String | `""` | Nombre de usuario del canal emisor en Twitch. |
| `TWITCH_BOT_LOGIN` | String | `""` | Nombre de usuario de la cuenta del bot en Twitch. |
| `TWITCH_ACCESS_TOKEN` | String | `""` | Token de acceso OAuth inicial manual (opcional). |
| `TWITCH_REFRESH_TOKEN` | String | `""` | Token de actualización OAuth inicial manual (opcional). |
| `TWITCH_TOKEN_FILE` | String | `.tokens.json` | Ruta local para el almacenamiento persistente de tokens OAuth. |
| `PERSONA_DIR` | String | `personas` | Directorio con las definiciones declarativas de personalidades YAML. |
| `PERSONA_FILE` | String | `personas/cohost_amigos.yaml` | Personalidad cargada por defecto al arrancar. |
| `STORE_PATH` | String | `data/social.db` | Ruta local del archivo JSON para la base de datos social atómica. |
| `GLOBAL_COOLDOWN_MS` | Entero | `3000` | Intervalo mínimo global entre mensajes del bot en milisegundos. |
| `USER_COOLDOWN_MS` | Entero | `15000` | Tiempo de enfriamiento de tasa por espectador en milisegundos. |
| `ROOM_BUFFER_SIZE` | Entero | `25` | Capacidad máxima de la ventana deslizante para mensajes ambientales. |
| `MAX_VIEWER_FACTS` | Entero | `15` | Límite por defecto de hechos episódicos guardados por espectador. |
| `ROOM_BUFFER_MAX_AGE_SEC` | Entero | `600` | Tiempo máximo de vida en segundos de mensajes ambientales en búfer. |
| `HTTP_ADDR` | String | `127.0.0.1:8080` | Dirección de red y puerto para la pasarela HTTP de diagnóstico. |

---

## Endpoints de Diagnóstico

El servidor HTTP interno expone rutas de diagnóstico y salud:

### `GET /healthz`
Sonda de salud para orquestadores y motores de contenedores:
```json
{
  "status": "ok"
}
```

### `GET /status`
Estado operativo detallado del motor en tiempo real:
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
        "display_name": "EspectadorUno",
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

Este proyecto está distribuido bajo los términos de la [Licencia MIT](LICENSE).

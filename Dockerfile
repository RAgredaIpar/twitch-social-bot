# Multi-stage Dockerfile para Twitch Social Bot
# Etapa 1: Compilación estática en Go puro (Zero CGO)
FROM golang:1.24-alpine AS builder

WORKDIR /build

# Copiar manifiesto de dependencias
COPY go.mod ./
RUN go mod download

# Copiar el código fuente
COPY . .

# Compilar binario estático sin CGO y con optimizaciones de tamaño
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-w -s" -o /build/bot ./cmd/bot

# Etapa 2: Imagen final minimalista y segura
FROM alpine:3.21

# Instalar certificados CA para TLS y zona horaria
RUN apk --no-cache add ca-certificates tzdata

# Crear usuario y grupo sin privilegios para ejecutar el bot
RUN addgroup -S botgroup && adduser -S botuser -G botgroup

WORKDIR /app

# Crear directorio de datos persistentes y asignar permisos al usuario no root
RUN mkdir -p /app/data && chown -R botuser:botgroup /app

# Copiar binario compilado y personalidades
COPY --from=builder /build/bot /app/bot
COPY personas /app/personas

USER botuser

# Exponer puerto HTTP de diagnóstico
EXPOSE 8080

# Comprobación de salud nativa contra el endpoint /healthz
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
  CMD wget -qO- http://127.0.0.1:8080/healthz || exit 1

ENTRYPOINT ["/app/bot"]

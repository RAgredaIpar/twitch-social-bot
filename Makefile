# ==============================================================================
# Makefile para Twitch Social Bot
# ==============================================================================

BIN_DIR := bin
BINARY := $(BIN_DIR)/bot
IMAGE_NAME := twitch-social-bot:latest

.PHONY: all build test run clean docker-build docker-run lint fmt

all: test build

## build: Compila el binario ejecutable en el directorio bin/
build:
	@mkdir -p $(BIN_DIR)
	go build -trimpath -ldflags="-w -s" -o $(BINARY) ./cmd/bot

## test: Ejecuta todas las pruebas unitarias y de integración con salida detallada
test:
	go test -v -race ./...

## test-coverage: Ejecuta pruebas con generación de reporte de cobertura
test-coverage:
	go test -coverprofile=coverage.txt -covermode=atomic ./...
	go tool cover -func=coverage.txt

## run: Ejecuta el bot en modo desarrollo utilizando las variables de .env
run:
	go run ./cmd/bot -env .env

## clean: Elimina binarios compilados y archivos temporales
clean:
	@rm -rf $(BIN_DIR) coverage.txt coverage.html

## fmt: Da formato a todos los archivos de código Go
fmt:
	go fmt ./...

## docker-build: Construye la imagen de contenedor Docker
docker-build:
	docker build -t $(IMAGE_NAME) .

## docker-run: Ejecuta el bot dentro del contenedor Docker con archivo .env y volumen persistente
docker-run:
	docker run --rm -it \
		--env-file .env \
		-p 8080:8080 \
		-v $(PWD)/data:/app/data \
		$(IMAGE_NAME)

# Сборка и запуск rocket. Основные цели:
#   make build   — собрать дашборд + бинарь bin/rocket (дашборд встроен)
#   make start   — собрать и запустить демон в фоне
#   make stop    — остановить демон
#   make status  — статус демона
#   make test    — go-тесты + web-тесты

BIN := bin/rocket

.PHONY: build web-build go-build start stop restart status test clean

build: web-build go-build

web-build:
	cd web && npm install --no-audit --no-fund && npm run build

# embed читает web/dist с диска в момент go build, поэтому порядок строгий:
# сначала настоящий билд SPA, потом go build, потом возвращаем tracked-плейсхолдер
# web/dist/index.html, чтобы дерево git оставалось чистым (ассеты в dist/ игнорируются).
go-build:
	go build -o $(BIN) ./cmd/rocket
	git checkout --quiet web/dist/index.html 2>/dev/null || true

start: build
	$(BIN) daemon start
	$(BIN) daemon status

stop:
	$(BIN) daemon stop 2>/dev/null || { [ -x $(BIN) ] || go build -o $(BIN) ./cmd/rocket; $(BIN) daemon stop; }

restart: stop start

status:
	$(BIN) daemon status

test:
	go test ./...
	cd web && npm test -- --run

clean:
	rm -rf bin

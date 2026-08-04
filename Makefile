# Сборка и запуск rocket. Основные цели:
#   make build   — собрать дашборд + бинарь bin/rocket (дашборд встроен)
#   make start   — собрать и запустить демон в фоне
#   make stop    — остановить демон
#   make status  — статус демона
#   make test    — go-тесты + web-тесты
#   make apk     — собрать Android APK мобильного приложения (mobile/)

BIN := bin/rocket
JAVA_HOME_ANDROID ?= /opt/homebrew/opt/openjdk@17
ANDROID_HOME ?= $(HOME)/Library/Android/sdk
APK := mobile/android/app/build/outputs/apk/release/app-release.apk

.PHONY: build web-build go-build install start stop restart status test clean apk

build: web-build go-build

web-build:
	cd web && npm install --no-audit --no-fund && npm run build

# embed читает web/dist с диска в момент go build, поэтому порядок строгий:
# сначала настоящий билд SPA, потом go build, потом возвращаем tracked-плейсхолдер
# web/dist/index.html, чтобы дерево git оставалось чистым (ассеты в dist/ игнорируются).
#
# codesign (только macOS): на arm64 ядро отказывается запускать бинарь с битой
# подписью и убивает процесс SIGKILL — снаружи это выглядит как молчаливый
# exit=137 без единой строки в логах. Линкер Go ставит adhoc-подпись сам, но
# она linker-signed и ломается от любой правки файла на месте; явный
# `codesign --force --sign -` кладёт нормальную adhoc-подпись поверх.
# Не-macOS и окружения без codesign шаг просто пропускают; на macOS с codesign
# ошибку не глушим — неподписанный бинарь потом молча умрёт при запуске.
go-build:
	go build -o $(BIN) ./cmd/rocket
	@if [ "$$(uname -s)" = Darwin ] && command -v codesign >/dev/null 2>&1; then codesign --force --sign - $(BIN); fi
	git checkout --quiet web/dist/index.html 2>/dev/null || true

# Глобальная команда `rocket`: симлинк на bin/rocket — каждый make build обновляет её автоматически.
# ВАЖНО: именно симлинк, а не копия. Замена установленного бинаря через `cp` пишет
# в тот же inode: у уже запущенного процесса страницы кода перестают сходиться с
# подписью, и ядро убивает его SIGKILL (exit=137), а новый запуск падает так же
# молча. Если копию всё же нужно положить — только `cp` во временный файл рядом и
# `mv` поверх (atomic rename меняет inode), либо `codesign --force --sign -` после cp.
install: build
	ln -sf $(CURDIR)/$(BIN) /opt/homebrew/bin/rocket

start: build
	$(BIN) daemon start
	$(BIN) daemon status

stop:
	$(BIN) daemon stop 2>/dev/null || { [ -x $(BIN) ] || go build -o $(BIN) ./cmd/rocket; $(BIN) daemon stop; }

# `daemon stop` blocks until rocketd has actually exited (see internal/daemon.WaitForExit),
# so start can run right after without racing a socket/pid file that hasn't been cleaned up yet.
restart: stop
	$(MAKE) start

status:
	$(BIN) daemon status

test:
	go test ./...
	cd web && npm test -- --run

clean:
	rm -rf bin

# APK мобильного приложения. Требует Android SDK и JDK 17
# (brew install openjdk@17). android/ генерируется expo prebuild при
# отсутствии; готовый APK — mobile/android/app/build/outputs/apk/release/.
apk:
	cd mobile && npm install --no-audit --no-fund --legacy-peer-deps
	[ -d mobile/android ] || (cd mobile && npx expo prebuild -p android --no-install)
	cd mobile/android && JAVA_HOME=$(JAVA_HOME_ANDROID) ANDROID_HOME=$(ANDROID_HOME) ./gradlew assembleRelease -q
	@echo "APK: $(APK)"

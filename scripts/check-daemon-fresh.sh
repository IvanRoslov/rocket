#!/bin/sh
# Падает (exit 1), если запущенный демон стартовал раньше, чем собран бинарь.
# Использование: check-daemon-fresh.sh <bin> <pidfile>
# Время старта процесса = now - etime (ps -o etime: [[dd-]hh:]mm:ss, переносимо
# между macOS и Linux); mtime бинаря: GNU stat -c, иначе BSD stat -f.
set -eu

bin=$1
pidfile=$2

if [ ! -r "$pidfile" ]; then
	echo "restart: pid-файл $pidfile не найден — демон не запущен" >&2
	exit 1
fi
pid=$(cat "$pidfile")
etime=$(ps -o etime= -p "$pid" 2>/dev/null | tr -d ' ') || true
if [ -z "$etime" ]; then
	echo "restart: процесс демона (pid $pid) не найден" >&2
	exit 1
fi

elapsed=$(echo "$etime" | awk -F'[-:]' '{
	if (NF == 4) s = $1*86400 + $2*3600 + $3*60 + $4
	else if (NF == 3) s = $1*3600 + $2*60 + $3
	else s = $1*60 + $2
	print s
}')
now=$(date +%s)
started=$((now - elapsed))
built=$(stat -c %Y "$bin" 2>/dev/null || stat -f %m "$bin")

# etime округляется до секунды — допускаем 2 с погрешности.
if [ $((started + 2)) -lt "$built" ]; then
	echo "restart: ОШИБКА — демон (pid $pid) запущен в $started, а $bin собран в $built: работает СТАРЫЙ бинарь" >&2
	exit 1
fi
echo "restart: ok — демон (pid $pid) моложе $bin"

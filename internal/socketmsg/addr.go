// Package socketmsg реализует минимальный клиент протокола cross-session
// messaging Claude Code CLI (>= 2.1.224): доставку сообщения в живую сессию
// через её unix-сокет.
//
// Протокол описан в docs/design/cc-socket-protocol.md.
package socketmsg

import (
	"fmt"
	"strings"
)

// AddrScheme — префикс адреса локального сокета в терминах Claude Code.
const AddrScheme = "uds:"

// EncodeAddr превращает путь к сокету в адрес вида `uds:<percent-encoded path>`.
//
// Набор литерально допустимых байт скопирован из бандла CLI: [A-Za-z0-9:_/.\-].
// Символ `%` в него не входит и сам кодируется, поэтому кодирование не
// идемпотентно — подавать сюда уже закодированный адрес нельзя.
func EncodeAddr(socketPath string) string {
	var b strings.Builder
	b.WriteString(AddrScheme)
	for i := 0; i < len(socketPath); i++ {
		c := socketPath[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9',
			c == ':', c == '_', c == '/', c == '.', c == '\\', c == '-':
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

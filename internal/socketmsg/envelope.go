package socketmsg

import (
	"regexp"
	"strings"
)

// EnvelopeTag — имя тега конверта, в который Claude Code заворачивает тело
// peer-сообщения перед показом получателю.
const EnvelopeTag = "cross-session-message"

// nameMaxRunes — предел длины from-name на стороне получателя (tSn/ms в бандле).
const nameMaxRunes = 20

var (
	subCloseTag  = regexp.MustCompile(`(?i)</(?:` + EnvelopeTag + `)(?:[>\s/]|$)`)
	nameSanitize = regexp.MustCompile(`[^\p{L}\p{N}._-]+`)
	nameTrim     = regexp.MustCompile(`^[._-]+|[._-]+$`)
	nameHasWord  = regexp.MustCompile(`[\p{L}\p{N}]`)
)

// SanitizeName приводит отображаемое имя отправителя к виду, который получатель
// не искажает при показе: только буквы/цифры/`.`/`_`/`-`, не длиннее 20 рун.
// Пустая строка означает «имя не пригодно, лучше не передавать его вовсе».
func SanitizeName(name string) string {
	s := nameTrim.ReplaceAllString(nameSanitize.ReplaceAllString(name, "-"), "")
	if !nameHasWord.MatchString(s) {
		return ""
	}
	if r := []rune(s); len(r) > nameMaxRunes {
		s = string(r[:nameMaxRunes])
	}
	return s
}

// Envelope собирает тело peer-сообщения: тег `cross-session-message` с
// атрибутами отправителя и телом на отдельной строке.
//
// Получатель парсит конверт строго и заново пересобирает его для сравнения,
// поэтому порядок атрибутов (from, from-session, hop-chain, from-name,
// from-mode) и одиночные переводы строк вокруг тела значимы.
func Envelope(from, fromName, body string) string {
	var attrs strings.Builder
	if from != "" {
		attrs.WriteString(` from="` + from + `"`)
	}
	if n := SanitizeName(fromName); n != "" {
		attrs.WriteString(` from-name="` + n + `"`)
	}
	safe := subCloseTag.ReplaceAllStringFunc(body, func(m string) string {
		return `<\/` + m[2:]
	})
	return "<" + EnvelopeTag + attrs.String() + ">\n" + safe + "\n</" + EnvelopeTag + ">"
}

// Преамбула и хвост, которыми Claude Code обрамляет конверт в транскрипте
// получателя. Кроме них вокруг конверта ничего быть не должно — иначе это
// не доставленное сообщение, а текст, который его цитирует.
const (
	transcriptPreamble = "Another Claude session sent a message:\n"
	transcriptTrailer  = "\n\nThis came from another Claude session"
)

var (
	envelopeOpen    = regexp.MustCompile(`\A<` + EnvelopeTag + `(?: [^>\n]*)?>\n`)
	escapedCloseTag = regexp.MustCompile(`(?i)<\\/(` + EnvelopeTag + `)`)
)

// Unwrap — обратная к Envelope операция над текстом user-записи транскрипта:
// снимает преамбулу Claude Code, конверт и хвост и возвращает тело в том виде,
// в каком его отправили (экранированные `<\/cross-session-message` —
// восстановлены). ok=false, если текст не является ровно одним доставленным
// конвертом; тогда вызывающий оставляет текст как есть.
func Unwrap(s string) (string, bool) {
	s = strings.TrimPrefix(s, transcriptPreamble)
	loc := envelopeOpen.FindStringIndex(s)
	if loc == nil {
		return "", false
	}
	rest := s[loc[1]:]
	closeTag := "\n</" + EnvelopeTag + ">"
	// Тело не может содержать неэкранированный закрывающий тег (Envelope его
	// экранирует), поэтому конец тела — первое вхождение.
	end := strings.Index(rest, closeTag)
	if end < 0 {
		return "", false
	}
	tail := rest[end+len(closeTag):]
	if tail != "" && !strings.HasPrefix(tail, transcriptTrailer) {
		return "", false
	}
	return escapedCloseTag.ReplaceAllString(rest[:end], "</$1"), true
}

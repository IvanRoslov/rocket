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

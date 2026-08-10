package socketmsg

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"runtime"
	"time"
)

// ProtocolVersion — значение поля msgV, которое пишет CLI 2.1.226.
const ProtocolVersion = 1

// DefaultTimeout — сколько ждём соединения и записи.
const DefaultTimeout = 5 * time.Second

// macOSLingerDelay — задержка перед закрытием записи на macOS. Сам CLI делает
// то же самое: на Darwin немедленный shutdown(WR) может обрубить данные,
// которые получатель ещё не вычитал.
const macOSLingerDelay = 50 * time.Millisecond

// Priority — место сообщения в очереди получателя.
type Priority string

const (
	// PriorityNow — обработать вне общей очереди, максимально быстро.
	PriorityNow Priority = "now"
	// PriorityNext — следующим сообщением (значение по умолчанию у CLI).
	PriorityNext Priority = "next"
	// PriorityLater — в конец очереди.
	PriorityLater Priority = "later"
)

// Message — то, что реально уходит в сокет одной JSON-строкой.
type Message struct {
	MsgV      int          `json:"msgV"`
	MsgID     string       `json:"msg_id"`
	Type      string       `json:"type"`
	Message   *UserContent `json:"message,omitempty"`
	Priority  Priority     `json:"priority,omitempty"`
	From      string       `json:"from,omitempty"`
	SessionID string       `json:"session_id,omitempty"`
	UUID      string       `json:"uuid,omitempty"`

	// Поля control-сообщений.
	Action    string `json:"action,omitempty"`
	Name      string `json:"name,omitempty"`
	Status    string `json:"status,omitempty"`
	Reason    string `json:"reason,omitempty"`
	OrigMsgID string `json:"orig_msg_id,omitempty"`
}

// UserContent — тело user-сообщения.
type UserContent struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Options настраивают отправку.
type Options struct {
	// From — путь к нашему собственному сокету (не адрес!). Пустая строка
	// означает «отправитель не адресуем»: квитанции о held/denied не придут.
	From string
	// FromName — отображаемое имя отправителя.
	FromName string
	// Priority — по умолчанию PriorityNext.
	Priority Priority
	// SessionID — если задан, получатель отбросит сообщение при несовпадении
	// со своим sessionId. Дешёвая защита от гонки «pid переиспользован».
	SessionID string
	// Timeout — по умолчанию DefaultTimeout.
	Timeout time.Duration
	// Raw отключает конверт cross-session-message: тело уходит как есть.
	Raw bool
	// MsgID — идентификатор сообщения. Пустая строка означает «сгенерировать».
	// Задавать его нужно, чтобы зарегистрировать ожидание квитанции ДО записи
	// в сокет: held отправляется получателем немедленно и способен обогнать
	// возврат из Send.
	MsgID string
}

// ErrEmptyText — CLI молча игнорирует user-сообщения с пустым content.
var ErrEmptyText = errors.New("socketmsg: пустой текст сообщения")

// Send доставляет текст в живую сессию Claude Code по пути её сокета.
// Возвращает msg_id — по нему можно сопоставить входящую квитанцию
// peer_message_status, если отправитель сам слушает свой сокет.
//
// Успешный возврат означает только «байты записаны и приняты», но не
// «сообщение показано пользователю»: приём может быть отложен (held) или
// отклонён (refused) политикой crossSessionInbound получателя.
func Send(socketPath, text string, opt Options) (string, error) {
	if text == "" {
		return "", ErrEmptyText
	}
	if opt.Priority == "" {
		opt.Priority = PriorityNext
	}
	from := ""
	if opt.From != "" {
		from = EncodeAddr(opt.From)
	}
	body := text
	if !opt.Raw {
		body = Envelope(from, opt.FromName, text)
	}
	id := opt.MsgID
	if id == "" {
		var err error
		if id, err = newUUID(); err != nil {
			return "", err
		}
	}
	msg := Message{
		MsgV:      ProtocolVersion,
		MsgID:     id,
		Type:      "user",
		Message:   &UserContent{Role: "user", Content: body},
		Priority:  opt.Priority,
		From:      from,
		SessionID: opt.SessionID,
	}
	if err := writeLine(socketPath, msg, opt.Timeout); err != nil {
		return "", err
	}
	return id, nil
}

// SendControl отправляет control-сообщение (например, переименование сессии).
func SendControl(socketPath string, msg Message, timeout time.Duration) error {
	id, err := newUUID()
	if err != nil {
		return err
	}
	msg.MsgV = ProtocolVersion
	msg.MsgID = id
	msg.Type = "control"
	return writeLine(socketPath, msg, timeout)
}

// Rename меняет отображаемое имя живой сессии.
func Rename(socketPath, name string, timeout time.Duration) error {
	return SendControl(socketPath, Message{Action: "rename", Name: name}, timeout)
}

// Probe проверяет, слушает ли кто-то сокет. Мёртвые сокеты остаются на диске
// после падения процесса, поэтому наличие файла ничего не гарантирует.
func Probe(socketPath string, timeout time.Duration) bool {
	if timeout <= 0 {
		timeout = 250 * time.Millisecond
	}
	conn, err := net.DialTimeout("unix", socketPath, timeout)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func writeLine(socketPath string, msg Message, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	payload, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	payload = append(payload, '\n')

	conn, err := net.DialTimeout("unix", socketPath, timeout)
	if err != nil {
		return fmt.Errorf("socketmsg: соединение с %s: %w", socketPath, err)
	}
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		return err
	}
	if _, err := conn.Write(payload); err != nil {
		return fmt.Errorf("socketmsg: запись в %s: %w", socketPath, err)
	}
	if runtime.GOOS == "darwin" {
		time.Sleep(macOSLingerDelay)
	}
	if uc, ok := conn.(*net.UnixConn); ok {
		return uc.CloseWrite()
	}
	return nil
}

// NewMsgID возвращает msg_id в формате, который принимает CLI (UUIDv4).
// Нужен, когда отправитель хочет подписаться на квитанцию до самой отправки.
func NewMsgID() (string, error) { return newUUID() }

func newUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

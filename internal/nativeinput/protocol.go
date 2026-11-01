package nativeinput

const (
	Version      = 1
	MaxFrameSize = 1 << 20
)

type Outcome string

const (
	NotSent  Outcome = "not_sent"
	Accepted Outcome = "accepted"
	Unknown  Outcome = "unknown"
)

type Target struct {
	SessionID  string
	InstanceID string
	SocketPath string
}

type Request struct {
	Version    int    `json:"v"`
	SessionID  string `json:"session_id"`
	InstanceID string `json:"instance_id"`
	MessageID  int64  `json:"message_id"`
	Text       string `json:"text"`
}

type response struct {
	Version    int    `json:"v"`
	SessionID  string `json:"session_id"`
	InstanceID string `json:"instance_id"`
	MessageID  int64  `json:"message_id"`
	Outcome    string `json:"outcome"`
}

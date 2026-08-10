package socketmsg

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
)

// Session — запись реестра живых сессий (~/.claude/sessions/<pid>.json).
// Поля соответствуют тому, что пишет сам CLI; неизвестные поля игнорируются.
type Session struct {
	PID                 int    `json:"pid"`
	SessionID           string `json:"sessionId"`
	CWD                 string `json:"cwd"`
	Name                string `json:"name"`
	NameSource          string `json:"nameSource"`
	Kind                string `json:"kind"`
	Entrypoint          string `json:"entrypoint"`
	Version             string `json:"version"`
	PeerProtocol        int    `json:"peerProtocol"`
	MessagingSocketPath string `json:"messagingSocketPath"`
	Tmux                string `json:"tmux"`
	Status              string `json:"status"`
	ProcStart           string `json:"procStart"`
	StartedAt           int64  `json:"startedAt"`
	UpdatedAt           int64  `json:"updatedAt"`
	StatusUpdatedAt     int64  `json:"statusUpdatedAt"`
}

// Addressable сообщает, есть ли у сессии сокет, в который можно писать.
func (s Session) Addressable() bool { return s.MessagingSocketPath != "" }

var pidFileRe = regexp.MustCompile(`^\d+\.json$`)

// SessionsDir возвращает каталог реестра сессий Claude Code.
func SessionsDir() string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return filepath.Join(d, "sessions")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".claude", "sessions")
	}
	return filepath.Join(home, ".claude", "sessions")
}

// ListSessions читает реестр. Битые и не относящиеся к делу файлы молча
// пропускаются: реестр обновляется конкурентно живыми процессами.
// Результат отсортирован по pid.
func ListSessions(dir string) ([]Session, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]Session, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !pidFileRe.MatchString(e.Name()) {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var s Session
		if err := json.Unmarshal(raw, &s); err != nil {
			continue
		}
		// Имя файла — источник истины для pid: CLI сам чинит расхождения.
		if pid, err := strconv.Atoi(e.Name()[:len(e.Name())-len(".json")]); err == nil {
			s.PID = pid
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PID < out[j].PID })
	return out, nil
}

// FindBySessionID ищет сессию по её sessionId (UUID из CLAUDE_CODE_SESSION_ID).
func FindBySessionID(dir, sessionID string) (Session, bool, error) {
	all, err := ListSessions(dir)
	if err != nil {
		return Session{}, false, err
	}
	for _, s := range all {
		if s.SessionID == sessionID {
			return s, true, nil
		}
	}
	return Session{}, false, nil
}

// What the thread-writing commands have in common (task #1023, spec v1
// §«Глаголы» and §«Подтверждение цели»). Task threads and role threads live in
// two files and two API paths, but the shape of a write — what it says, where
// it lands, and how the result is reported back — is one thing, written once
// here so the two can never drift apart.
package cli

import (
	"fmt"
	"strconv"
	"strings"
)

// Shared flag help, so all six writing commands describe a flag identically.
const (
	dismissFlagUsage = "закрыть тред без ответа (можно с текстом-причиной)"
	chooseFlagUsage  = "закрыть тред выбором варианта по его номеру (1-based)"
	dryRunFlagUsage  = "показать цель записи и ничего не отправлять"
	joinFlagUsage    = "войти в тред, участником которого вы не являетесь (осознанно)"
	optionFlagUsage  = "вариант ответа (можно повторять)"
	fyiFlagUsage     = "статусная заметка: тред создаётся закрытым и никого не ждёт"
)

// threadReplyOptions is a reply: text, optional addressees, and the two
// confirmation flags that exist because a reply CAN land in the wrong thread.
type threadReplyOptions struct {
	body   string
	to     []string
	join   bool
	dryRun bool
}

func (o threadReplyOptions) requestBody() map[string]any {
	req := map[string]any{"body": o.body}
	setTo(req, o.to)
	setConfirmations(req, o.join, o.dryRun)
	return req
}

// threadCloseOptions is a close, in one of three mutually exclusive flavours:
// an answer, a choice among the thread's options, or a dismissal.
type threadCloseOptions struct {
	body    string
	choose  int
	dismiss bool
	to      []string
	join    bool
	dryRun  bool
}

// validate rejects the combinations that would leave the resolution ambiguous.
// Silently preferring one of two given resolutions is how a thread ends up
// closed with an answer its author never wrote.
func (o threadCloseOptions) validate(usage string) error {
	if o.choose < 0 {
		return &usageError{message: usage}
	}
	given := 0
	if o.choose > 0 {
		given++
	}
	// A dismissal may carry a reason, so the two count as one way of closing;
	// a bare answer counts separately.
	if o.dismiss {
		given++
	} else if o.body != "" {
		given++
	}
	if given != 1 {
		return &usageError{message: usage}
	}
	return nil
}

func (o threadCloseOptions) requestBody() map[string]any {
	req := map[string]any{}
	switch {
	case o.dismiss:
		req["dismiss"] = true
		// The reason is optional; when given it is what the participants are
		// told, so dropping it would make "--dismiss <почему>" a lie.
		if o.body != "" {
			req["body"] = o.body
		}
	case o.choose > 0:
		req["choose"] = o.choose
	default:
		req["body"] = o.body
	}
	setTo(req, o.to)
	setConfirmations(req, o.join, o.dryRun)
	return req
}

// setConfirmations attaches join/dry_run only when asked for, so an ordinary
// write stays byte-identical to what the CLI sent before they existed.
func setConfirmations(req map[string]any, join, dryRun bool) {
	if join {
		req["join"] = true
	}
	if dryRun {
		req["dry_run"] = true
	}
}

// askRequestBody builds the body of a thread-opening request. Absent flags add
// no keys at all: a client that asks nothing new must produce the request it
// always produced.
func askRequestBody(body, context string, to, options []string, fyi bool) map[string]any {
	req := map[string]any{"body": body}
	if context != "" {
		req["context"] = context
	}
	setTo(req, to)
	if len(options) > 0 {
		req["options"] = options
	}
	if fyi {
		req["type"] = "fyi"
	}
	return req
}

// validateAskFlags rejects options on an fyi thread: fyi is a status note that
// is born closed and waits on nobody, so answer choices have nothing to attach
// to and would silently never be shown.
func validateAskFlags(options []string, fyi bool, usage string) error {
	if fyi && len(options) > 0 {
		return &usageError{message: usage +
			"\n--fyi — статусная заметка, её никто не отвечает: варианты (--option) к ней не применимы"}
	}
	return nil
}

// threadWriteResult is what the two thread response shapes have in common for
// reporting a write back — enough to say what happened and where.
type threadWriteResult interface {
	ref() string
	echo() string
	dryRun() bool
}

func (q questionRow) ref() string {
	return threadRef(q.LocalRef, strconv.FormatInt(q.TaskID, 10), q.Ordinal)
}
func (q questionRow) echo() string { return q.Echo }
func (q questionRow) dryRun() bool { return q.DryRun }

func (q agentQuestionRow) ref() string  { return threadRef(q.LocalRef, q.RoleID, q.Ordinal) }
func (q agentQuestionRow) echo() string { return q.Echo }
func (q agentQuestionRow) dryRun() bool { return q.DryRun }

// renderWriteResult renders the line a write prints. It always names the
// target — the echo when the daemon sent one, the local ref otherwise — so a
// write into the wrong thread is visible at once instead of hours later. A dry
// run reports the target it WOULD have written to and says plainly that it
// wrote nothing.
func renderWriteResult(action string, res threadWriteResult) string {
	var sb strings.Builder
	if res.dryRun() {
		sb.WriteString("dry-run: ничего не отправлено\n")
	} else {
		fmt.Fprintf(&sb, "%s %s\n", action, res.ref())
	}
	if e := res.echo(); e != "" {
		sb.WriteString(e + "\n")
	} else if res.dryRun() {
		// Nothing to echo (an older daemon): the ref is the only target
		// confirmation available, and a dry run must still show one.
		sb.WriteString(res.ref() + "\n")
	}
	return sb.String()
}

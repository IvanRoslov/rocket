// Команда ccsend — ручной драйвер спайка: посмотреть живые сессии Claude Code
// и доставить в одну из них сообщение через unix-сокет, без tmux.
//
//	ccsend list
//	ccsend send <socket-path|pid|sessionId> <текст>
//	ccsend listen <socket-path>   # приём квитанций peer_message_status
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strconv"
	"text/tabwriter"
	"time"

	"github.com/IvanRoslov/rocket/internal/socketmsg"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	var err error
	switch os.Args[1] {
	case "list":
		err = cmdList()
	case "send":
		if len(os.Args) < 4 {
			usage()
		}
		err = cmdSend(os.Args[2], os.Args[3])
	case "listen":
		if len(os.Args) < 3 {
			usage()
		}
		err = cmdListen(os.Args[2])
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "ccsend:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: ccsend list | ccsend send <socket|pid|sessionId> <текст> | ccsend listen <socket>")
	os.Exit(2)
}

func cmdList() error {
	sessions, err := socketmsg.ListSessions(socketmsg.SessionsDir())
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "PID\tALIVE\tKIND\tNAME\tSTATUS\tPROTO\tSOCKET\tCWD")
	for _, s := range sessions {
		alive := "-"
		if s.Addressable() {
			alive = "dead"
			if socketmsg.Probe(s.MessagingSocketPath, 250*time.Millisecond) {
				alive = "live"
			}
		}
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%d\t%s\t%s\n",
			s.PID, alive, s.Kind, s.Name, s.Status, s.PeerProtocol, s.MessagingSocketPath, s.CWD)
	}
	return w.Flush()
}

// resolve принимает путь к сокету, pid или sessionId и возвращает путь к сокету
// плюс sessionId получателя (для защитного поля session_id).
func resolve(target string) (socket, sessionID string, err error) {
	if len(target) > 0 && target[0] == '/' {
		return target, "", nil
	}
	sessions, err := socketmsg.ListSessions(socketmsg.SessionsDir())
	if err != nil {
		return "", "", err
	}
	pid, isPID := 0, false
	if n, convErr := strconv.Atoi(target); convErr == nil {
		pid, isPID = n, true
	}
	for _, s := range sessions {
		if (isPID && s.PID == pid) || s.SessionID == target {
			if !s.Addressable() {
				return "", "", fmt.Errorf("у сессии %d нет messagingSocketPath", s.PID)
			}
			return s.MessagingSocketPath, s.SessionID, nil
		}
	}
	return "", "", fmt.Errorf("сессия %q не найдена в %s", target, socketmsg.SessionsDir())
}

func cmdSend(target, text string) error {
	socket, sessionID, err := resolve(target)
	if err != nil {
		return err
	}
	id, err := socketmsg.Send(socket, text, socketmsg.Options{
		From:      os.Getenv("CCSEND_REPLY_SOCKET"),
		FromName:  envOr("CCSEND_NAME", "rocket"),
		SessionID: sessionID,
	})
	if err != nil {
		return err
	}
	fmt.Printf("отправлено в %s, msg_id=%s\n", socket, id)
	return nil
}

// cmdListen поднимает сокет отправителя, чтобы увидеть квитанции о судьбе
// сообщения. Путь должен лежать в том же каталоге, что и сокет получателя,
// и оканчиваться на .sock — иначе получатель откажется слать квитанцию.
func cmdListen(path string) error {
	_ = os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return err
	}
	defer ln.Close()
	fmt.Printf("слушаю %s (адрес %s)\n", path, socketmsg.EncodeAddr(path))
	for {
		conn, err := ln.Accept()
		if err != nil {
			return err
		}
		go func() {
			defer conn.Close()
			sc := bufio.NewScanner(conn)
			sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
			for sc.Scan() {
				var m socketmsg.Message
				if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
					fmt.Println("не-JSON:", sc.Text())
					continue
				}
				fmt.Printf("%s action=%s status=%s orig=%s from=%s\n",
					m.Type, m.Action, m.Status, m.OrigMsgID, m.From)
			}
		}()
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

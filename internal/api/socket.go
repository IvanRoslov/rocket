package api

import (
	"errors"
	"fmt"
	"net"
	"os"
	"syscall"
	"time"
)

// ErrSocketInUse reports that the unix socket path is already claimed: a
// live rocketd is serving it, or another start is mid-claim. Starting a
// second daemon on the same socket is not allowed — and, crucially, the
// loser must never unlink the leader's socket, which would leave the live
// daemon serving a listener no client can reach any more (every `rocket`
// command dials the path, not the inode).
var ErrSocketInUse = errors.New("socket already in use by a running rocketd")

// socketProbeTimeout bounds the connect used to decide whether a socket
// file is backed by a live listener. A local unix connect either succeeds
// or is refused immediately; anything slower means "can't prove it's dead",
// and we refuse rather than delete.
const socketProbeTimeout = 500 * time.Millisecond

// listenUnixSingleton binds a unix listener on path, acting as the
// singleton claim for the daemon.
//
// Order matters, and it is the opposite of the obvious one: probe/claim
// first, delete only what is provably dead. It takes an exclusive lock on
// <path>.lock so the probe and the bind can't interleave with another
// starting daemon, then connects to path: a successful connect means a
// live daemon owns it (ErrSocketInUse, file untouched); a refused connect
// means the file is a leftover from an uncleanly terminated process and is
// safe to unlink before binding.
func listenUnixSingleton(path string) (net.Listener, error) {
	unlock, err := lockSocketPath(path)
	if err != nil {
		return nil, err
	}
	defer unlock()

	alive, err := socketAlive(path)
	if err != nil {
		return nil, err
	}
	if alive {
		return nil, fmt.Errorf("%s: %w", path, ErrSocketInUse)
	}

	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("remove stale socket: %w", err)
	}

	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("listen unix %s: %w", path, err)
	}
	if err := os.Chmod(path, 0600); err != nil {
		ln.Close() // unlinks the socket we just created
		return nil, fmt.Errorf("chmod socket: %w", err)
	}
	return ln, nil
}

// socketAlive reports whether path is a socket with a listener accepting
// connections. A missing path, or one occupied by something that isn't a
// socket, is not alive. A connect error other than "refused" is treated as
// alive: unlinking a socket we can't prove is dead is the failure mode
// this whole function exists to prevent.
func socketAlive(path string) (bool, error) {
	fi, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("stat socket %s: %w", path, err)
	}
	if fi.Mode()&os.ModeSocket == 0 {
		return false, nil
	}

	conn, err := net.DialTimeout("unix", path, socketProbeTimeout)
	if err == nil {
		conn.Close()
		return true, nil
	}
	if errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.ENOENT) {
		return false, nil
	}
	return true, nil
}

// lockSocketPath takes an exclusive, non-blocking flock on <path>.lock and
// returns a func releasing it. The lock covers only the probe-and-bind
// window, so two daemons starting at the same instant can't both conclude
// the socket is stale and both unlink it. Failing to take the lock means
// another start is already inside that window.
func lockSocketPath(path string) (func(), error) {
	lockPath := path + ".lock"
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("open socket lock %s: %w", lockPath, err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, fmt.Errorf("%s is being claimed by another rocketd: %w", path, ErrSocketInUse)
		}
		return nil, fmt.Errorf("lock %s: %w", lockPath, err)
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}, nil
}

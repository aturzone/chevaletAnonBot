// Package health provides a minimal TCP liveness endpoint that mirrors the
// Python health_check_app (modules/Global/jobs.py): it listens on a port and
// accepts then immediately closes connections, so a successful TCP connect from
// the Docker healthcheck means "the bot process is alive".
package health

import (
	"fmt"
	"net"
)

// Listener owns the health TCP socket.
type Listener struct {
	ln net.Listener
}

// Listen starts accepting connections on localhost:port in a background
// goroutine. Each connection is closed immediately.
func Listen(port int) (*Listener, error) {
	ln, err := net.Listen("tcp", fmt.Sprintf("localhost:%d", port))
	if err != nil {
		return nil, err
	}
	h := &Listener{ln: ln}
	go h.serve()
	return h, nil
}

func (h *Listener) serve() {
	for {
		conn, err := h.ln.Accept()
		if err != nil {
			return // listener closed
		}
		_ = conn.Close()
	}
}

// Close stops the listener.
func (h *Listener) Close() error { return h.ln.Close() }

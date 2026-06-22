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

// Listen starts accepting connections on 127.0.0.1:port in a background
// goroutine. Each connection is closed immediately.
//
// The address is the IPv4 loopback EXPLICITLY (not "localhost"): in a dual-stack
// container /etc/hosts often maps "localhost" to ::1 as well, and Go's net.Listen
// binds a single resolved address — so "localhost" could bind only [::1]:port
// while the Docker healthcheck (nc -z 127.0.0.1) probes IPv4, marking a live bot
// unhealthy and restart-looping it at cutover. The Python original pinned IPv4
// the same way (socket.AF_INET + bind("localhost", …)).
func Listen(port int) (*Listener, error) {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
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

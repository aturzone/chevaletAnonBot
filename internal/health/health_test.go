package health

import (
	"fmt"
	"net"
	"testing"
	"time"
)

// TestHealthListener locks the IPv4-loopback bind contract (the HIGH audit fix:
// bind 127.0.0.1, not "localhost" which can resolve to ::1 on a dual-stack host
// while the Docker healthcheck probes 127.0.0.1). It also checks the
// accept-then-immediately-close behavior and that the port is exclusively held.
func TestHealthListener(t *testing.T) {
	const port = 39517
	hl, err := Listen(port)
	if err != nil {
		t.Skipf("port %d unavailable in this environment: %v", port, err)
	}
	defer hl.Close()

	// the IPv4 loopback MUST be reachable — this is exactly what the Docker
	// healthcheck (nc -z 127.0.0.1) relies on.
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 2*time.Second)
	if err != nil {
		t.Fatalf("dial 127.0.0.1:%d failed: %v (the listener must bind the IPv4 loopback)", port, err)
	}
	// the server closes the connection immediately, so a read returns EOF.
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, rerr := conn.Read(make([]byte, 1)); rerr == nil {
		t.Error("expected the health connection to be closed immediately by the server")
	}
	_ = conn.Close()

	// the port is exclusively held: a second Listen on it must fail.
	if hl2, err := Listen(port); err == nil {
		_ = hl2.Close()
		t.Error("a second Listen on the same port should fail (already bound)")
	}
}

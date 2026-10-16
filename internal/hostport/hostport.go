// Package hostport finds free TCP ports on the host's loopback interface,
// where msb publishes Sandbox ports by default.
package hostport

import (
	"fmt"
	"net"
	"strconv"
)

// OrcaWebUI is the first port tried for a Sandbox's Orca web UI.
const OrcaWebUI = 6768

// FirstFree returns the first port, counting up from start, that can be bound
// on 127.0.0.1. The port is released before returning, so a caller racing
// another process can still lose it.
func FirstFree(start int) (int, error) {
	for port := start; port <= 65535; port++ {
		l, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
		if err != nil {
			continue
		}
		l.Close()
		return port, nil
	}
	return 0, fmt.Errorf("no free port on 127.0.0.1 from %d", start)
}

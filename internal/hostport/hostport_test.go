package hostport_test

import (
	"net"
	"strconv"
	"testing"

	"github.com/Betree/Orcasm/internal/hostport"
)

func listen(t *testing.T, port int) net.Listener {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	return l
}

func TestFirstFreeSkipsPortsInUse(t *testing.T) {
	taken := listen(t, 0).Addr().(*net.TCPAddr).Port

	got, err := hostport.FirstFree(taken)
	if err != nil {
		t.Fatal(err)
	}
	if got <= taken {
		t.Fatalf("want a port above the taken %d, got %d", taken, got)
	}
	listen(t, got) // the returned port must actually be bindable
}

func TestFirstFreeReturnsStartWhenFree(t *testing.T) {
	l := listen(t, 0)
	free := l.Addr().(*net.TCPAddr).Port
	l.Close()

	got, err := hostport.FirstFree(free)
	if err != nil {
		t.Fatal(err)
	}
	if got != free {
		t.Fatalf("want %d, got %d", free, got)
	}
}

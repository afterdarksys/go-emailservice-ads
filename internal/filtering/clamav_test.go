package filtering

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"os"
	"testing"
)

func TestClamAVRequiresExplicitCleanVerdict(t *testing.T) {
	for _, reply := range []string{"stream: OK\x00", "stream: Eicar-Test-Signature FOUND\x00", "INSTREAM size limit exceeded. ERROR\x00", ""} {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		done := make(chan struct{})
		go func() {
			defer close(done)
			c, e := l.Accept()
			if e != nil {
				return
			}
			defer c.Close()
			command := make([]byte, 10)
			if _, e = io.ReadFull(c, command); e != nil {
				return
			}
			if string(command) != "zINSTREAM\x00" {
				t.Error("wrong stream command")
				return
			}
			for {
				var n uint32
				if e = binary.Read(c, binary.BigEndian, &n); e != nil {
					return
				}
				if n == 0 {
					break
				}
				if _, e = io.CopyN(io.Discard, c, int64(n)); e != nil {
					return
				}
			}
			io.WriteString(c, reply)
		}()
		found, e := ScanClamAV(context.Background(), l.Addr().String(), make([]byte, 70000))
		l.Close()
		<-done
		switch reply {
		case "stream: OK\x00":
			if e != nil || found {
				t.Fatal(found, e)
			}
		case "stream: Eicar-Test-Signature FOUND\x00":
			if e != nil || !found {
				t.Fatal(found, e)
			}
		default:
			if e == nil {
				t.Fatal("incomplete scan accepted")
			}
		}
	}
}

func TestLiveClamAV(t *testing.T) {
	addr := os.Getenv("MAILHUB_CLAMAV_TEST_ADDR")
	if addr == "" {
		t.Skip("set MAILHUB_CLAMAV_TEST_ADDR for isolated live scanner test")
	}
	if err := ClamAVHealth(context.Background(), addr); err != nil {
		t.Fatal(err)
	}
	if found, err := ScanClamAV(context.Background(), addr, []byte("routine message")); err != nil || found {
		t.Fatal(found, err)
	}
	// EICAR is the standard harmless antivirus test string.
	payload := []byte("X5O!P%@AP[4\\PZX54(P^)7CC)7}$EICAR-STANDARD-ANTIVIRUS-TEST-FILE!$H+H*")
	if found, err := ScanClamAV(context.Background(), addr, payload); err != nil || !found {
		t.Fatal(found, err)
	}
}

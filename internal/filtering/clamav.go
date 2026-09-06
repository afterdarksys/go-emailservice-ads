package filtering

import (
	"bufio"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strings"
	"time"
)

func clamConn(ctx context.Context, address string) (net.Conn, func(), error) {
	conn, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, nil, err
	}
	deadline := time.Now().Add(15 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	conn.SetDeadline(deadline)
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	return conn, func() { stop(); conn.Close() }, nil
}
func clamReply(conn net.Conn) (string, error) {
	r := bufio.NewReader(io.LimitReader(conn, 4097))
	raw, err := r.ReadString(0)
	if err != nil {
		return "", err
	}
	if len(raw) > 4096 {
		return "", fmt.Errorf("oversized ClamAV response")
	}
	return strings.TrimSuffix(raw, "\x00"), nil
}
func ClamAVHealth(ctx context.Context, address string) error {
	conn, close, err := clamConn(ctx, address)
	if err != nil {
		return err
	}
	defer close()
	if _, err = io.WriteString(conn, "zPING\x00"); err != nil {
		return err
	}
	reply, err := clamReply(conn)
	if err != nil {
		return err
	}
	if reply != "PONG" {
		return fmt.Errorf("invalid ClamAV health response")
	}
	return nil
}

// ScanClamAV requires an explicit clean result. Transport errors, limits, and
// incomplete scans never become an implicit clean verdict.
func ScanClamAV(ctx context.Context, address string, data []byte) (bool, error) {
	conn, close, err := clamConn(ctx, address)
	if err != nil {
		return false, err
	}
	defer close()
	if _, err = io.WriteString(conn, "zINSTREAM\x00"); err != nil {
		return false, err
	}
	var size [4]byte
	for len(data) > 0 {
		n := len(data)
		if n > 65536 {
			n = 65536
		}
		binary.BigEndian.PutUint32(size[:], uint32(n))
		if _, err = conn.Write(size[:]); err != nil {
			return false, err
		}
		if _, err = io.Copy(conn, strings.NewReader(string(data[:n]))); err != nil {
			return false, err
		}
		data = data[n:]
	}
	if _, err = conn.Write([]byte{0, 0, 0, 0}); err != nil {
		return false, err
	}
	reply, err := clamReply(conn)
	if err != nil {
		return false, err
	}
	if reply == "stream: OK" {
		return false, nil
	}
	if strings.HasPrefix(reply, "stream: ") && strings.HasSuffix(reply, " FOUND") {
		return true, nil
	}
	return false, fmt.Errorf("ClamAV did not complete scan: %s", reply)
}

// ClamAV VERSION includes the timestamp of the loaded signature database.
func ClamAVDatabaseTime(ctx context.Context, address string) (time.Time, error) {
	c, close, err := clamConn(ctx, address)
	if err != nil {
		return time.Time{}, err
	}
	defer close()
	if _, err = io.WriteString(c, "zVERSION\x00"); err != nil {
		return time.Time{}, err
	}
	reply, err := clamReply(c)
	if err != nil {
		return time.Time{}, err
	}
	parts := strings.Split(reply, "/")
	if len(parts) < 3 {
		return time.Time{}, fmt.Errorf("missing antivirus database timestamp")
	}
	return time.Parse("Mon Jan _2 15:04:05 2006", strings.TrimSpace(parts[len(parts)-1]))
}

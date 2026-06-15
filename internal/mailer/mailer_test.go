package mailer

import (
	"bufio"
	"net"
	"strings"
	"testing"
	"time"
)

// fakeSMTP accepts one message and returns the raw DATA it received.
func fakeSMTP(t *testing.T) (addr string, got chan string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	got = make(chan string, 1)
	go func() {
		defer ln.Close()
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(5 * time.Second))
		r := bufio.NewReader(conn)
		w := func(s string) { conn.Write([]byte(s + "\r\n")) }
		w("220 fake ESMTP")
		var data strings.Builder
		inData := false
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			if inData {
				if line == ".\r\n" {
					inData = false
					w("250 OK")
					got <- data.String()
					continue
				}
				data.WriteString(line)
				continue
			}
			cmd := strings.ToUpper(strings.TrimSpace(line))
			switch {
			case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
				w("250-fake")
				w("250 OK")
			case strings.HasPrefix(cmd, "MAIL"), strings.HasPrefix(cmd, "RCPT"):
				w("250 OK")
			case strings.HasPrefix(cmd, "DATA"):
				w("354 go ahead")
				inData = true
			case strings.HasPrefix(cmd, "QUIT"):
				w("221 bye")
				return
			default:
				w("250 OK")
			}
		}
	}()
	return ln.Addr().String(), got
}

func TestSendWithAttachment(t *testing.T) {
	addr, got := fakeSMTP(t)
	host, port := splitHostPort(addr)
	cfg := Config{Host: host, Port: port, From: "lib@polka.test", Security: "none"}
	if !cfg.Valid() {
		t.Fatal("config should be valid")
	}
	err := cfg.Send("kindle@example.com", "Война и мир", "hi", "voyna.fb2", []byte("BOOKDATA"))
	if err != nil {
		t.Fatal(err)
	}
	select {
	case msg := <-got:
		for _, want := range []string{"To: kindle@example.com", "multipart/mixed",
			`filename="voyna.fb2"`, "base64", "Qk9PS0RBVEE="} { // base64("BOOKDATA")
			if !strings.Contains(msg, want) {
				t.Errorf("message missing %q", want)
			}
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no message received")
	}
}

func splitHostPort(addr string) (string, int) {
	h, p, _ := net.SplitHostPort(addr)
	port := 0
	for _, c := range p {
		port = port*10 + int(c-'0')
	}
	return h, port
}

package server

import (
	"bufio"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func fakeSMTP(t *testing.T) (host string, port int, got chan string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	got = make(chan string, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(5 * time.Second))
		r := bufio.NewReader(conn)
		w := func(s string) { conn.Write([]byte(s + "\r\n")) }
		w("220 fake")
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
			case strings.HasPrefix(cmd, "DATA"):
				w("354 ok")
				inData = true
			case strings.HasPrefix(cmd, "QUIT"):
				w("221 bye")
				return
			default:
				w("250 OK")
			}
		}
	}()
	h, p, _ := net.SplitHostPort(ln.Addr().String())
	port = 0
	for _, c := range p {
		port = port*10 + int(c-'0')
	}
	return h, port, got
}

func TestSendBookByEmail(t *testing.T) {
	ts, client, _ := newManageServer(t)

	resp := uploadFiles(t, client, ts.URL+"/admin/books/upload", map[string][]byte{
		"voyna.fb2": []byte(opdsFB2),
	}, nil)
	var up map[string][]uploadResult
	json.NewDecoder(resp.Body).Decode(&up)
	resp.Body.Close()
	bookID := up["results"][0].BookID

	send := func() int {
		r, _ := client.Post(ts.URL+"/api/v1/books/"+itoa64(bookID)+"/send", "application/json", nil)
		defer r.Body.Close()
		return r.StatusCode
	}

	// No SMTP configured -> 503
	if code := send(); code != http.StatusServiceUnavailable {
		t.Fatalf("send without smtp -> %d", code)
	}

	// Configure SMTP at the fake server + enable
	host, port, got := fakeSMTP(t)
	postJSON(t, client, ts.URL+"/admin/settings", map[string]any{
		"smtp": map[string]any{
			"enabled": true, "host": host, "port": port,
			"from": "lib@polka.test", "security": "none",
		},
	}).Body.Close()

	// No reader email yet -> 400
	if code := send(); code != http.StatusBadRequest {
		t.Fatalf("send without reader email -> %d", code)
	}

	// Set reader email
	postJSON(t, client, ts.URL+"/api/v1/me/reader-email", map[string]any{"email": "me@kindle.com"}).Body.Close()

	if code := send(); code != http.StatusOK {
		t.Fatalf("send -> %d, want 200", code)
	}
	select {
	case msg := <-got:
		for _, want := range []string{"To: me@kindle.com", "multipart/mixed", "base64"} {
			if !strings.Contains(msg, want) {
				t.Errorf("sent message missing %q", want)
			}
		}
	case <-time.After(5 * time.Second):
		t.Fatal("fake smtp received nothing")
	}
}

func TestSecretRoundtrip(t *testing.T) {
	s := &Server{secret: make([]byte, 32)}
	for i := range s.secret {
		s.secret[i] = byte(i)
	}
	enc := s.encryptSecret("hunter2")
	if enc == "" || enc == "hunter2" {
		t.Fatal("not encrypted")
	}
	if dec := s.decryptSecret(enc); dec != "hunter2" {
		t.Errorf("roundtrip = %q", dec)
	}
}

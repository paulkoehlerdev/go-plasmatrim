package main

import (
	"crypto/subtle"
	"flag"
	"io"
	"log"
	"log/slog"
	"net"
	"strings"

	"github.com/gliderlabs/ssh"
	xssh "golang.org/x/crypto/ssh"
)

func main() {
	var addr string
	flag.StringVar(&addr, "addr", ":2222", "Address to listen on")
	flag.Parse()

	if addr == "" {
		flag.Usage()
		return
	}

	slog.Info("Starting SSH server", "addr", addr)

	ssh.Handle(func(s ssh.Session) {
		cmd := strings.Join(s.Command(), " ")
		if cmd == "" {
			cmd = "<interactive-shell>"
		}
		slog.Info("SSH session",
			"addr", s.RemoteAddr().String(),
			"user", s.User(),
			"command", cmd,
		)
		io.WriteString(s, "Hello world\n")
	})

	log.Fatal(ssh.ListenAndServe(addr, nil, func(server *ssh.Server) error {
		server.ConnCallback = func(ctx ssh.Context, conn net.Conn) net.Conn {
			slog.Info("connection", "addr", conn.RemoteAddr().String(), "local", conn.LocalAddr())
			return conn
		}

		server.PasswordHandler = func(ctx ssh.Context, password string) bool {
			// Intentionally keep false for a real password-blocking server.
			// For a honeypot, return true if you want to keep interaction going.
			allow := subtle.ConstantTimeCompare([]byte(password), []byte("letmein")) == 1
			slog.Info("password attempt",
				"addr", ctx.RemoteAddr().String(),
				"user", ctx.User(),
				"password", password,
				"accepted", allow,
			)
			return false
		}

		server.PublicKeyHandler = func(ctx ssh.Context, key ssh.PublicKey) bool {
			slog.Info("public key attempt",
				"addr", ctx.RemoteAddr().String(),
				"user", ctx.User(),
				"fingerprint", xssh.FingerprintSHA256(key),
				"type", key.Type(),
			)
			return false
		}

		return nil
	}))
}

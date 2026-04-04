package main

import (
	"io"
	"log"
	"log/slog"

	"github.com/gliderlabs/ssh"
)

func main() {
	addr := ":2222"

	slog.Info("Starting SSH server", "addr", addr)

	ssh.Handle(func(s ssh.Session) {
		io.WriteString(s, "Hello world\n")
		slog.Info("SSH session", "addr", s.RemoteAddr().String(), "user", s.User())
	})

	log.Fatal(ssh.ListenAndServe(addr, nil))
}

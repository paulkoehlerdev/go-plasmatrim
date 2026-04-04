package main

import (
	"flag"
	"io"
	"log"
	"log/slog"
	"net"

	"github.com/gliderlabs/ssh"
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
		io.WriteString(s, "Hello world\n")
		slog.Info("SSH session", "addr", s.RemoteAddr().String(), "user", s.User())
	})

	log.Fatal(ssh.ListenAndServe(addr, nil, func(server *ssh.Server) error {
		server.ConnCallback = func(ctx ssh.Context, conn net.Conn) net.Conn {
			slog.Info("connection", "addr", conn.RemoteAddr())
			return conn
		}
		return nil
	}))
}

package main

import (
	"encoding/json"
	"flag"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gliderlabs/ssh"
	"github.com/gorilla/websocket"
	xssh "golang.org/x/crypto/ssh"
)

type SSHConnectionInfo struct {
	EventType            string `json:"event_type"`
	Timestamp            string `json:"timestamp"`
	RemoteAddr           string `json:"remote_addr"`
	LocalAddr            string `json:"local_addr"`
	User                 string `json:"user,omitempty"`
	Command              string `json:"command,omitempty"`
	Password             string `json:"password,omitempty"`
	PublicKeyFingerprint string `json:"public_key_fingerprint,omitempty"`
	PublicKeyType        string `json:"public_key_type,omitempty"`
}

type websocketClient struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

type websocketHub struct {
	mu       sync.RWMutex
	clients  map[*websocketClient]struct{}
	upgrader websocket.Upgrader
}

func newWebsocketHub() *websocketHub {
	return &websocketHub{
		clients: make(map[*websocketClient]struct{}),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		},
	}
}

func (h *websocketHub) serveWs(w http.ResponseWriter, r *http.Request) {
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("failed to upgrade websocket", "error", err)
		return
	}

	client := &websocketClient{conn: conn}
	h.mu.Lock()
	h.clients[client] = struct{}{}
	h.mu.Unlock()

	slog.Info("websocket client connected", "remote", conn.RemoteAddr())
	defer h.removeClient(client)

	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			break
		}
	}
}

func (h *websocketHub) removeClient(client *websocketClient) {
	h.mu.Lock()
	delete(h.clients, client)
	h.mu.Unlock()
	client.conn.Close()
}

func (h *websocketHub) broadcast(event SSHConnectionInfo) {
	payload, err := json.Marshal(event)
	if err != nil {
		slog.Error("failed to marshal ssh event", "error", err)
		return
	}

	h.mu.RLock()
	clients := make([]*websocketClient, 0, len(h.clients))
	for client := range h.clients {
		clients = append(clients, client)
	}
	h.mu.RUnlock()

	for _, client := range clients {
		client.mu.Lock()
		err := client.conn.WriteMessage(websocket.TextMessage, payload)
		client.mu.Unlock()
		if err != nil {
			slog.Error("failed to write websocket message", "error", err)
			h.removeClient(client)
		}
	}
}

func (h *websocketHub) emit(eventType, remoteAddr, localAddr, user string) {
	h.broadcast(SSHConnectionInfo{
		EventType:  eventType,
		Timestamp:  time.Now().UTC().Format(time.RFC3339Nano),
		RemoteAddr: remoteAddr,
		LocalAddr:  localAddr,
		User:       user,
	})
}

func (h *websocketHub) emitSessionInfo(s ssh.Session, cmd string) {
	h.broadcast(SSHConnectionInfo{
		EventType:  "session_open",
		Timestamp:  time.Now().UTC().Format(time.RFC3339Nano),
		RemoteAddr: s.RemoteAddr().String(),
		Command:    cmd,
	})
}

func (h *websocketHub) emitPasswordInfo(addr, user, password string) {
	h.broadcast(SSHConnectionInfo{
		EventType:  "password_attempt",
		Timestamp:  time.Now().UTC().Format(time.RFC3339Nano),
		RemoteAddr: addr,
		User:       user,
		Password:   password,
	})
}

func (h *websocketHub) emitPublicKeyInfo(addr, user string, key ssh.PublicKey) {
	h.broadcast(SSHConnectionInfo{
		EventType:            "public_key_attempt",
		Timestamp:            time.Now().UTC().Format(time.RFC3339Nano),
		RemoteAddr:           addr,
		User:                 user,
		PublicKeyFingerprint: xssh.FingerprintSHA256(key),
		PublicKeyType:        key.Type(),
	})
}

func main() {
	var addr string
	var wsAddr string
	flag.StringVar(&addr, "addr", ":2222", "Address to listen on")
	flag.StringVar(&wsAddr, "ws-addr", ":8080", "WebSocket listen address")
	flag.Parse()

	if addr == "" {
		flag.Usage()
		return
	}

	websocketHub := newWebsocketHub()
	http.HandleFunc("/ws", websocketHub.serveWs)
	go func() {
		slog.Info("Starting websocket endpoint", "addr", wsAddr, "path", "/ws")
		if err := http.ListenAndServe(wsAddr, nil); err != nil {
			log.Fatal(err)
		}
	}()

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
		go websocketHub.emitSessionInfo(s, cmd)
		io.WriteString(s, "Hello World!\n")
	})

	log.Fatal(ssh.ListenAndServe(addr, nil, func(server *ssh.Server) error {
		server.ConnCallback = func(ctx ssh.Context, conn net.Conn) net.Conn {
			remoteAddr := conn.RemoteAddr().String()
			localAddr := conn.LocalAddr().String()
			slog.Info("connection", "addr", remoteAddr, "local", localAddr)
			go websocketHub.emit("connection", remoteAddr, localAddr, ctx.User())
			return conn
		}

		server.PasswordHandler = func(ctx ssh.Context, password string) bool {
			slog.Info("password attempt",
				"addr", ctx.RemoteAddr().String(),
				"user", ctx.User(),
				"password", password,
			)
			go websocketHub.emitPasswordInfo(ctx.RemoteAddr().String(), ctx.User(), password)
			return true
		}

		server.PublicKeyHandler = func(ctx ssh.Context, key ssh.PublicKey) bool {
			slog.Info("public key attempt",
				"addr", ctx.RemoteAddr().String(),
				"user", ctx.User(),
				"fingerprint", xssh.FingerprintSHA256(key),
				"type", key.Type(),
			)
			go websocketHub.emitPublicKeyInfo(ctx.RemoteAddr().String(), ctx.User(), key)
			return true
		}

		return nil
	}))
}

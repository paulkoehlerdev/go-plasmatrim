package main

import (
	"encoding/json"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/gorilla/websocket"
)

type SSHConnectionInfo struct {
	EventType            string `json:"event_type"`
	Timestamp            string `json:"timestamp"`
	RemoteAddr           string `json:"remote_addr"`
	User                 string `json:"user,omitempty"`
	Command              string `json:"command,omitempty"`
	Password             string `json:"password,omitempty"`
	PublicKeyFingerprint string `json:"public_key_fingerprint,omitempty"`
	PublicKeyType        string `json:"public_key_type,omitempty"`
}

func main() {
	var wsURL string
	flag.StringVar(&wsURL, "ws-url", "ws://127.0.0.1:8080/ws", "Websocket URL to connect to")
	flag.Parse()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		slog.Error("failed to connect to websocket", "url", wsURL, "error", err)
		os.Exit(1)
	}
	defer conn.Close()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(stop)

	slog.Info("connected to websocket", "url", wsURL)

	done := make(chan struct{})
	go func() {
		<-stop
		close(done)
		conn.Close()
	}()

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			select {
			case <-done:
				return
			default:
				slog.Error("websocket read error", "error", err)
				os.Exit(1)
			}
		}

		var event SSHConnectionInfo
		if err := json.Unmarshal(message, &event); err != nil {
			slog.Error("invalid websocket payload", "error", err, "payload", string(message))
			continue
		}

		slog.Info("received event",
			"event_type", event.EventType,
			"timestamp", event.Timestamp,
			"remote_addr", event.RemoteAddr,
			"user", event.User,
		)
		handleEventForAnimation(event)
	}
}

func handleEventForAnimation(event SSHConnectionInfo) {
	// TODO: connect and animate PlasmaTrim HID device with events.
	_ = event
}

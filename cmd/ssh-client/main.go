package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sstallion/go-hid"
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

const (
	plasmaTrimVendorID  = 0x26f3
	plasmaTrimProductID = 0x1000
)

var lightAnimationMu sync.Mutex
var pendingLightProfile eventLightProfile
var lightAnimationRunning bool

type eventLightProfile struct {
	red            uint8
	green          uint8
	blue           uint8
	peakBrightness brightness
	frameDelay     time.Duration
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
	profile := lightProfileForEvent(event.EventType)
	lightAnimationMu.Lock()
	pendingLightProfile = mergeLightProfiles(pendingLightProfile, profile)

	if !lightAnimationRunning {
		lightAnimationRunning = true
		go animateLightBar()
	}
	lightAnimationMu.Unlock()
}

func lightProfileForEvent(eventType string) eventLightProfile {
	switch strings.ToLower(eventType) {
	case "connection":
		return eventLightProfile{
			red:            0xf8,
			green:          0xf8,
			blue:           0xf8,
			peakBrightness: 25,
			frameDelay:     55 * time.Millisecond,
		}
	case "session_open":
		return eventLightProfile{
			red:            0xff,
			green:          0x00,
			blue:           0x00,
			peakBrightness: 95,
			frameDelay:     140 * time.Millisecond,
		}
	case "password_attempt":
		return eventLightProfile{
			red:            0xff,
			green:          0x5a,
			blue:           0x00,
			peakBrightness: 45,
			frameDelay:     90 * time.Millisecond,
		}
	case "public_key_attempt":
		return eventLightProfile{
			red:            0xff,
			green:          0x0d,
			blue:           0x0d,
			peakBrightness: 65,
			frameDelay:     110 * time.Millisecond,
		}
	default:
		return eventLightProfile{
			red:            0x22,
			green:          0x22,
			blue:           0x22,
			peakBrightness: 15,
			frameDelay:     800 * time.Millisecond,
		}
	}
}

func animateLightBar() {
	device, err := hid.OpenFirst(plasmaTrimVendorID, plasmaTrimProductID)
	if err != nil {
		slog.Warn("failed to open plasma trim device", "error", err)
		lightAnimationMu.Lock()
		lightAnimationRunning = false
		lightAnimationMu.Unlock()
		return
	}
	defer device.Close()

	const leds = 8
	var activeProfile eventLightProfile
	var frameDelay time.Duration = 800 * time.Millisecond
	headPosition := 0
	level := 0
	stepsRemaining := leds

	for {
		lightAnimationMu.Lock()
		incoming := pendingLightProfile
		pendingLightProfile = eventLightProfile{}

		if incoming.peakBrightness > 0 {
			activeProfile = mergeLightProfiles(activeProfile, incoming)
			if incoming.frameDelay > 0 {
				frameDelay = minDuration(frameDelay, incoming.frameDelay)
			}
			if int(incoming.peakBrightness) > level {
				level = int(incoming.peakBrightness)
			}
		}

		if level <= 0 {
			lightAnimationRunning = false
			lightAnimationMu.Unlock()
			_ = setColor(device, "#000000", 0)
			return
		}
		lightAnimationMu.Unlock()

		currentFrameDelay := frameDelay
		if currentFrameDelay == 0 {
			currentFrameDelay = 800 * time.Millisecond
		}

		frameLevel := level * stepsRemaining / leds
		if err := setRunningColor(device, activeProfile, frameLevel, headPosition); err != nil {
			slog.Warn("failed to set plasma trim color", "error", err)
			lightAnimationMu.Lock()
			lightAnimationRunning = false
			lightAnimationMu.Unlock()
			return
		}

		time.Sleep(currentFrameDelay)
		stepsRemaining--
		headPosition++
		level = maxInt(level-1, 0)

		if stepsRemaining <= 0 {
			lightAnimationMu.Lock()
			lightAnimationRunning = false
			lightAnimationMu.Unlock()
			_ = setColor(device, "#000000", 0)
			return
		}
	}
}

func colorValueForProfile(profile eventLightProfile) string {
	return fmt.Sprintf("#%02x%02x%02x", profile.red, profile.green, profile.blue)
}

func mergeLightProfiles(base, extra eventLightProfile) eventLightProfile {
	merged := base

	merged.frameDelay = minDuration(base.frameDelay, extra.frameDelay)

	combinedBrightness := maxUint8(byte(base.peakBrightness), byte(extra.peakBrightness))
	merged.peakBrightness = brightness(combinedBrightness)

	switch {
	case byte(extra.peakBrightness) > byte(base.peakBrightness):
		merged.red = extra.red
		merged.green = extra.green
		merged.blue = extra.blue
	case byte(extra.peakBrightness) < byte(base.peakBrightness):
		merged.red = base.red
		merged.green = base.green
		merged.blue = base.blue
	default:
		merged.red = maxUint8(base.red, extra.red)
		merged.green = maxUint8(base.green, extra.green)
		merged.blue = maxUint8(base.blue, extra.blue)
	}

	return merged
}

func setRunningColor(device *hid.Device, profile eventLightProfile, level, head int) error {
	var colors [24]byte

	for i := 0; i < 8; i++ {
		intensity := 0
		if i == head {
			intensity = 255
		} else if i == head-1 {
			intensity = 80
		} else if i == head-2 {
			intensity = 40
		}

		colors[i*3+0] = uint8((int(profile.red) * level * intensity) / (255 * 100))
		colors[i*3+1] = uint8((int(profile.green) * level * intensity) / (255 * 100))
		colors[i*3+2] = uint8((int(profile.blue) * level * intensity) / (255 * 100))
	}

	return setColorBytes(device, colors, 100)
}

func minDuration(a, b time.Duration) time.Duration {
	if a == 0 {
		return b
	}
	if b == 0 {
		return a
	}
	if a < b {
		return a
	}
	return b
}

func maxUint8(a, b uint8) uint8 {
	if a > b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// brightness is a percentage of the maximum brightness. [0-100]
type brightness uint8

func (b brightness) String() string {
	return strconv.Itoa(int(b)) + "%"
}

func getBrightness(device *hid.Device) (brightness, error) {
	var cmd [32]byte

	cmd[0] = 0x0C

	if _, err := device.Write(cmd[:]); err != nil {
		return 0, err
	}

	if _, err := device.Read(cmd[:]); err != nil {
		return 0, err
	}

	return brightness(cmd[1]), nil
}

func setBrightness(device *hid.Device, brightness brightness) error {
	var cmd [32]byte

	cmd[0] = 0x0B
	cmd[1] = byte(brightness)

	if _, err := device.Write(cmd[:]); err != nil {
		return err
	}

	err := readData(device)
	if err != nil {
		return err
	}

	return nil
}

func getColor(device *hid.Device) (string, error) {
	var cmd [32]byte

	cmd[0] = 0x01

	if _, err := device.Write(cmd[:]); err != nil {
		return "", err
	}

	_, err := device.Read(cmd[:])
	if err != nil {
		return "", err
	}

	var hexData strings.Builder
	const hexChars = "0123456789ABCDEF"
	hexData.Grow(48)
	for i := 0; i < 24; i++ {
		v := cmd[i+1]
		hexData.WriteByte(hexChars[v>>4])
		hexData.WriteByte(hexChars[v&0x0F])
	}

	return fmt.Sprintf("%+v", cmd), nil
}

func setColor(device *hid.Device, colorValue string, value brightness) error {
	parsed, err := htmlColorToBytes(colorValue)
	if err != nil {
		return err
	}

	return setColorBytes(device, parsed, value)
}

func setColorBytes(device *hid.Device, color [24]byte, value brightness) error {
	var cmd [33]byte

	cmd[0] = 0x00
	cmd[1] = 0x00
	copy(cmd[2:], color[:])
	cmd[26] = byte(value)

	if _, err := device.Write(cmd[:]); err != nil {
		return err
	}

	err := readData(device)
	if err != nil {
		return err
	}
	return nil
}

func htmlColorToBytes(color string) ([24]byte, error) {
	color = strings.Join(strings.Fields(color), "")
	color = strings.TrimPrefix(color, "#")

	var compactColor string
	switch len(color) {
	case 3:
		// e.g. F00 -> FF0000 then duplicated for all 8 LEDs
		raw := []byte{color[0], color[0], color[1], color[1], color[2], color[2]}
		compactColor = strings.Repeat(string(raw), 8)
	case 6:
		// e.g. FF0000 for one LED, replicated across all 8 LEDs
		compactColor = strings.Repeat(color, 8)
	case 24:
		// e.g. 24 nibble hex for each LED's RGB values (R G B per LED)
		expanded := make([]byte, 48)
		for i := 0; i < 24; i++ {
			expanded[i*2] = color[i]
			expanded[i*2+1] = color[i]
		}
		compactColor = string(expanded)
	case 48:
		compactColor = color
	default:
		return [24]byte{}, fmt.Errorf("unsupported html color length %d", len(color))
	}

	var colorBytes [24]byte
	for i := 0; i < 24; i++ {
		v, err := strconv.ParseUint(compactColor[i*2:i*2+2], 16, 8)
		if err != nil {
			return colorBytes, fmt.Errorf("invalid html color at byte %d: %w", i, err)
		}
		colorBytes[i] = byte(v)
	}

	return colorBytes, nil
}

func readData(dev *hid.Device) error {
	var err error
	var buf [32]byte

	_, err = dev.Read(buf[:])
	if err != nil {
		return err
	}

	return nil
}

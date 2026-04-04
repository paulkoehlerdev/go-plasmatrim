package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/sstallion/go-hid"
)

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

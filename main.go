package main

import (
	"fmt"

	"github.com/sstallion/go-hid"
)

func main() {
	dev, err := hid.OpenFirst(0x26f3, 0x1000)
	if err != nil {
		panic(err)
	}
	defer dev.Close()

	info, err := dev.GetDeviceInfo()
	if err != nil {
		panic(err)
	}
	fmt.Println(info)

	setBrightness(dev, 30)

	err = setColor(dev, "F00FF00F00FF00FF0FF00FF0", 30)
	if err != nil {
		panic(err)
	}

	color, err := getColor(dev)
	if err != nil {
		panic(err)
	}
	fmt.Println(color)

	fmt.Println("done")
}

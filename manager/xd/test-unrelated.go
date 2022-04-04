package main

import (
	"fmt"
	"io/ioutil"
	"strings"

	"github.com/Acetolyne/keylogger"
)

// FindKeyboardDevice gets the keyboard device path to listen to
func FindKeyboardDevice() string {
	path := "/sys/class/input/event%d/device/name"
	resolved := "/dev/input/event%d"

	for i := 0; i < 255; i++ {
		buff, err := ioutil.ReadFile(fmt.Sprintf(path, i))
		if err != nil {
			continue
		}

		deviceName := strings.ToLower(string(buff))

		//fmt.Printf("Device: %q\n", deviceName)

		if deviceName == "mosart semi. 2.4g wireless keypad\n" {
			return fmt.Sprintf(resolved, i)
		}
	}

	return ""
}

func main() {
	path := FindKeyboardDevice()

	logger, err := keylogger.New(path)
	if err != nil {
		panic(err)
	}

	ch := logger.Read()

	for v := range ch {
		if v.Type == keylogger.EvKey {
			fmt.Printf("key: %v - %x\n", keyCodeMap[v.Code], v.Value)
		}
	}
}

package main

import (
	"fmt"

	"github.com/notnotquinn/wts/manager"
)

func main() {
	m, err := manager.NewManager("./manager/test/conf.yaml")
	if err != nil {
		panic(err)
	}

	fmt.Printf("m.Config: %#v\n", m.Config)
}

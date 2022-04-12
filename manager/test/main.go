package main

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/k0kubun/pp"
	"github.com/notnotquinn/wts"
	"github.com/notnotquinn/wts/manager"
)

func main() {
	m, errs := manager.NewManager("./manager/test/conf.yaml")
	if len(errs) != 0 {

		if len(errs) == 1 {
			panic(errs[0])
		}

		fmt.Fprintf(os.Stderr, "%d errors:\n", len(errs))
		for i, err := range errs {
			fmt.Fprintf(os.Stderr, "  %d: %s\n", i+1, err.Error())
		}

		panic(fmt.Sprintf("%d errors: see above\n", len(errs)))
	}

	pp.Println(m)
	panic(http.ListenAndServe(fmt.Sprintf(":%d", m.Config.HubPort), m))

	emit, _ := createEmitter(4044, m.HubURL())

	time.Sleep(time.Second / 10)

	emit(DummyEmitterThingy{
		FoobarXd: "this is FoobarXd",
		Lmao:     178,
	})
}

type DummyEmitterThingy struct {
	FoobarXd string `json:"foobar"`
	Lmao     int    `json:"integer"`
}

func createEmitter(port int, hubUrl string) (emit func(d DummyEmitterThingy), node *wts.Node) {
	baseURL := fmt.Sprintf("http://localhost:%d/", port)
	bindAddr := fmt.Sprintf("127.0.0.1:%d", port)
	n := wts.NewNode(baseURL, hubUrl)

	ch := make(chan DummyEmitterThingy)
	wts.AddEmitter(n, wts.NewBasicEmitter("dummy", ch))

	go http.ListenAndServe(bindAddr, n)
	return func(d DummyEmitterThingy) {
		ch <- d
	}, n
}

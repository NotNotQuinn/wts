package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"

	"github.com/notnotquinn/go-websub"
	"github.com/notnotquinn/wts/manager"
)

var log = websub.Logger()

func main() {
	m, errs := manager.New("./manager/test/conf.yaml")
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

	mu := sync.Mutex{}

	m.AddSniffer("", func(topic, contentType string, body io.Reader) {
		// content, err := io.ReadAll(body)
		// if err != nil {
		// 	log.Err(err).Msg("failed to read sniffed body")
		// }

		mu.Lock()

		log.Debug().
			// Str("content", string(content)).
			Str("topic", topic).
			// Str("content-type", contentType).
			Msg("[hub] sniffed topic")

		// pp.Println(m.Node)

		mu.Unlock()
	})

	go http.ListenAndServe(fmt.Sprintf(":%d", m.Config.HubPort), m)

	err := m.Node.SubscribeAll()
	if err != nil {
		panic(err)
	}

	<-make(chan struct{})
}

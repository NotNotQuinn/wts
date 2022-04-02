package main

import (
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/notnotquinn/go-websub"
	"github.com/notnotquinn/wts"
)

var log = websub.Logger()

type ActorMsg struct {
	Sound string `json:"sound"`
}

type EmitterMsg struct {
	Xd int `json:"xd"`
}

func main() {
	hubURL, _ := startHub(8080)
	node := startNode("node", 4044, hubURL)

	err := node.SubscribeAll()
	if err != nil {
		panic(err)
	}

	log.Debug().Msgf("[node] Subscribed")

	actor := wts.NewFuncActor(
		"test",
		func(msg *wts.EventPayload[ActorMsg]) (ok bool) {
			log.Debug().Msgf("[node:actor:test] ShouldAct()")
			return true
		},
		func(msg *wts.EventPayload[ActorMsg]) (ok bool) {
			log.Debug().Msgf("[node:actor:test] Act()")
			return true
		},
	)

	ch := make(chan EmitterMsg)

	go func(ch chan EmitterMsg) {
		t := time.NewTicker(time.Second * 4)
		i := 0
		for {
			<-t.C
			ch <- EmitterMsg{
				Xd: i,
			}
			i++
		}
	}(ch)

	emitter := wts.NewBasicEmitter("test", ch)

	// Actors and Emitters may have the same name
	// Add an actor to a node
	err = wts.AddActor(node, actor)
	if err != nil {
		panic(err)
	}

	// Add an emitter to a node
	err = wts.AddEmitter(node, emitter)
	if err != nil {
		panic(err)
	}

	err = node.Broadcast(node.BaseURL()+"/test/request", ActorMsg{
		Sound: "xd",
	})
	if err != nil {
		panic(err)
	}

	node2 := startNode("node2", 4045, hubURL)

	err = node2.SubscribeAll()
	if err != nil {
		panic(err)
	}

	log.Debug().Msgf("[node2] Subscribed")

	err = wts.AddExternalActor[ActorMsg](node2, "http://localhost:4044/test")
	if err != nil {
		panic(err)
	}

	err = wts.AddExternalEmitter[EmitterMsg](node2, "http://localhost:4044/test")
	if err != nil {
		panic(err)
	}

	err = node2.Broadcast("http://localhost:4044/test/request", ActorMsg{
		Sound: "xd",
	})

	if err != nil {
		panic(err)
	}

	<-make(chan struct{})
}

func startNode(loggingName string, port int, hubURL string) *wts.Node {
	self := fmt.Sprintf("http://localhost:%d/", port)
	n := wts.NewNode(self, hubURL)

	go http.ListenAndServe(fmt.Sprintf("127.0.0.1:%d", port), n)
	log.Debug().Msgf("[%s] Listening on %s", loggingName, self)

	return n
}

// starts hub at localhost:port (not exposed)
func startHub(port int) (hubURL string, hub *websub.Hub) {
	self := fmt.Sprintf("http://localhost:%d/", port)
	h := websub.NewHub(
		self,
		websub.HubExposeTopics(true),
		websub.HubAllowPostBodyAsContent(true),
		websub.HubWithHashFunction("sha512"),
	)

	h.AddSniffer("", func(topic, contentType string, body io.Reader) {
		if topic == h.HubURL()+"/topics" {
			return // ignore
		}

		// content, err := io.ReadAll(body)
		// if err != nil {
		// 	panic(err)
		// }

		log.Debug().
			Str("topic", topic).
			// Str("content", string(content)).
			Msgf("[hub] new publish")
	})

	go http.ListenAndServe(fmt.Sprintf("127.0.0.1:%d", port), h)
	log.Debug().Msgf("[hub] Listening on %s", self)

	return self, h
}

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
	//////////////////////////////////////
	////             HUB              ////
	//////////////////////////////////////
	hubURL, _ := startHub(8080)
	//////////////////////////////////////
	////     NODE 1  (broadcaster)    ////
	//////////////////////////////////////
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
	////////////////////////////////////////////////////
	////      NODE 2 (third-party broadcaster)      ////
	////////////////////////////////////////////////////

	node2 := startNode("node2", 4045, hubURL)

	err = node2.SubscribeAll()
	if err != nil {
		panic(err)
	}

	log.Debug().Msgf("[node2] Subscribed")
	//////////////////////////////////////
	////      NODE 3   (listener)     ////
	//////////////////////////////////////
	node3 := startNode("node3", 4046, hubURL)

	if err = node3.SubscribeAll(); err != nil {
		panic(err)
	}
	log.Debug().Msgf("[node3] Subscribed")

	_, err = wts.AddActorHook(
		node3, "http://localhost:4044/test",
		func(msg wts.EventPayload[ActorMsg]) {
			log.Debug().Msg("[node3:hook] requested")
		},
		func(msg wts.EventPayload[ActorMsg]) {
			log.Debug().Msg("[node3:hook] executed")
		},
	)
	if err != nil {
		panic(err)
	}

	broadcastEventOnNode2, err := wts.AddActorHook[ActorMsg](
		node2, "http://localhost:4044/test",
		nil, nil,
	)
	if err != nil {
		panic(err)
	}

	broadcastDataOnNode2, err := wts.AddEmitterHook[EmitterMsg](
		node2, "http://localhost:4044/test",
		nil,
	)
	if err != nil {
		panic(err)
	}
	_, err = wts.AddEmitterHook(
		node2, "http://localhost:4044/test",
		func(msg wts.EventPayload[EmitterMsg]) {
			log.Debug().Msg("[node3:hook] data")
		},
	)
	if err != nil {
		panic(err)
	}

	time.Sleep(time.Second)

	err = broadcastEventOnNode2(ActorMsg{
		Sound: "xd",
	})
	if err != nil {
		panic(err)
	}

	time.Sleep(time.Second / 30)

	err = broadcastEventOnNode2(ActorMsg{Sound: "xd"})
	if err != nil {
		panic(err)
	}

	err = broadcastDataOnNode2(EmitterMsg{
		Xd: 90999,
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

		content, err := io.ReadAll(body)
		if err != nil {
			panic(err)
		}

		log.Debug().
			Str("topic", topic).
			Str("content", string(content)).
			Msgf("[hub] new publish")
	})

	go http.ListenAndServe(fmt.Sprintf("127.0.0.1:%d", port), h)
	log.Debug().Msgf("[hub] Listening on %s", self)

	return self, h
}

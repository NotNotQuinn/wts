package wts

import (
	"errors"
)

// AddActor adds an actor to the node
func AddActor[MsgType any](node *Node, a Actor[MsgType]) error {
	proxy := newActorProxy(a)
	actorURL := node.baseURL + "/" + a.Name()

	node.mu.RLock()
	_, exists := node.emitters[actorURL]
	node.mu.RUnlock()

	if exists {
		return errors.New("actor already exists")
	}

	if node.subscribed {
		err := node.subscribeTopic(actorURL + "/" + string(Request))
		if err != nil {
			return err
		}
	}

	node.mu.Lock()
	node.actors[actorURL] = proxy
	node.mu.Unlock()
	return nil
}

// AddEmitter adds an emitter to the node
func AddEmitter[MsgType any](node *Node, e Emitter[MsgType]) error {
	proxy := newEmitterProxy(e)
	emitterURL := node.baseURL + "/" + e.Name()

	node.mu.RLock()
	_, exists := node.emitters[emitterURL]
	node.mu.RUnlock()

	if exists {
		return errors.New("emitter already exists")
	}

	node.mu.Lock()
	node.emitters[emitterURL] = proxy
	node.mu.Unlock()

	go func(node *Node, emitterURL string, ch <-chan MsgType) {
		for msg := range ch {
			err := node.Broadcast(emitterURL+"/"+string(Data), msg)
			if err != nil {
				log.Err(err).
					Str("emitterURL", emitterURL).
					Msg("could not broadcast data event")
			}
		}
	}(node, emitterURL, e.DataEvents())

	return nil
}

// AddExternalEmitter adds an external emitter which can we can listen to events from
func AddExternalEmitter[MsgType any](node *Node, emitterURL string) error {
	emitterEncoding := NewEncoderProxy[MsgType]()
	node.mu.RLock()
	proxy, exists := node.external[emitterURL]
	node.mu.RUnlock()
	if exists {
		if proxy.emitter != nil {
			return errors.New("external emitter already exists")
		}
		proxy.emitter = emitterEncoding
	} else {
		proxy := &externalProxy{
			actor:   nil,
			emitter: emitterEncoding,
		}
		node.mu.Lock()
		node.external[emitterURL] = proxy
		node.mu.Unlock()
	}
	return nil
}

// AddExternalActor adds an external actor which can be requested to act
func AddExternalActor[MsgType any](node *Node, actorURL string) error {
	actorEncoding := NewEncoderProxy[MsgType]()
	node.mu.RLock()
	proxy, exists := node.external[actorURL]
	node.mu.RUnlock()
	if exists {
		if proxy.actor != nil {
			return errors.New("external emitter already exists")
		}
		proxy.actor = actorEncoding
	} else {
		proxy := &externalProxy{
			actor:   actorEncoding,
			emitter: nil,
		}
		node.mu.Lock()
		node.external[actorURL] = proxy
		node.mu.Unlock()
	}
	return nil
}

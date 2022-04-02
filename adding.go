package wts

import (
	"errors"
)

// AddActor adds an actor to the node
func AddActor[MsgType any](node *Node, a Actor[MsgType]) error {
	proxy := newActorProxy(a)
	actorUrl := node.baseUrl + "/" + a.Name()

	node.mu.RLock()
	_, exists := node.emitters[actorUrl]
	node.mu.RUnlock()

	if exists {
		return errors.New("actor already exists")
	}

	if node.subscribed {
		err := node.subscribeTopic(actorUrl + "/" + string(Request))
		if err != nil {
			return err
		}
	}

	node.mu.Lock()
	node.actors[actorUrl] = proxy
	node.mu.Unlock()
	return nil
}

// AddEmitter adds an emitter to the node
func AddEmitter[MsgType any](node *Node, e Emitter[MsgType]) error {
	proxy := newEmitterProxy(e)
	emitterUrl := node.baseUrl + "/" + e.Name()

	node.mu.RLock()
	_, exists := node.emitters[emitterUrl]
	node.mu.RUnlock()

	if exists {
		return errors.New("emitter already exists")
	}

	node.mu.Lock()
	node.emitters[emitterUrl] = proxy
	node.mu.Unlock()

	go func(node *Node, emitterUrl string, ch <-chan MsgType) {
		for msg := range ch {
			err := node.Broadcast(emitterUrl+"/"+string(Data), msg)
			if err != nil {
				log.Err(err).
					Str("emitterUrl", emitterUrl).
					Msg("could not broadcast data event")
			}
		}
	}(node, emitterUrl, e.DataEvents())

	return nil
}

// AddExternalActor adds an external actor which can we can listen to events from
func AddExternalEmitter[MsgType any](node *Node, emitterUrl string) error {
	emitterEncoding := NewEncoderProxy[MsgType]()
	node.mu.RLock()
	proxy, exists := node.external[emitterUrl]
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
		node.external[emitterUrl] = proxy
		node.mu.Unlock()
	}
	return nil
}

// AddExternalActor adds an external actor which can be requested to act
func AddExternalActor[MsgType any](node *Node, actorUrl string) error {
	actorEncoding := NewEncoderProxy[MsgType]()
	node.mu.RLock()
	proxy, exists := node.external[actorUrl]
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
		node.external[actorUrl] = proxy
		node.mu.Unlock()
	}
	return nil
}

package wts

import (
	"errors"
)

func AddActor[MsgType any](node *Node, a Actor[MsgType]) error {
	proxy := newActorProxy(a)
	actorURL := node.baseURL + "/" + a.Name()

	node.actorsMu.RLock()
	_, exists := node.actors[actorURL]
	node.actorsMu.RUnlock()

	if exists {
		return errors.New("actor already exists")
	}

	if node.subscribed {
		err := node.subscribeTopic(actorURL + "/" + string(Request))
		if err != nil {
			return err
		}
	}

	node.actorsMu.Lock()
	node.actors[actorURL] = proxy
	node.actorsMu.Unlock()
	return nil
}

func AddEmitter[MsgType any](node *Node, e Emitter[MsgType]) error {
	proxy := newEmitterProxy(e)
	emitterURL := node.baseURL + "/" + e.Name()

	node.emittersMu.RLock()
	_, exists := node.emitters[emitterURL]
	node.emittersMu.RUnlock()

	if exists {
		return errors.New("emitter already exists")
	}

	node.emittersMu.Lock()
	node.emitters[emitterURL] = proxy
	node.emittersMu.Unlock()

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

type OnEventFunc[MsgType any] func(eventURL string, msg *EventPayload[MsgType])
type eventHook struct {
	*encoderProxy
	happened func(eventURL string, msg *EventPayload[any]) error
}

func AddEmitterHook[MsgType any](
	node *Node,
	actorURL string,
	onData OnEventFunc[MsgType],
) (broadcastData func(msg MsgType) error, err error) {
	encoder := NewEncoderProxy[MsgType]()

	if onData == nil {
		// Add dummy event to keep the encoder for sending messages
		onData = func(eventURL string, msg *EventPayload[MsgType]) {}
	}

	if onData != nil {
		hook := newEventHook(onData)
		hook.encoderProxy = encoder

		node.hooksMu.RLock()
		_, ok := node.hooks[actorURL]
		node.hooksMu.RUnlock()

		node.hooksMu.Lock()
		if !ok {
			node.hooks[actorURL] = map[EventType]*eventHook{
				Data: hook,
			}
		} else {
			node.hooks[actorURL][Data] = hook
		}
		node.hooksMu.Unlock()

		if node.subscribed {
			err := node.subscribeTopic(actorURL + "/" + string(Data))
			if err != nil {
				return nil, err
			}
		}
	}

	return func(msg MsgType) error {
		return node.Broadcast(actorURL+"/"+string(Data), msg)
	}, nil
}

func AddActorHook[MsgType any](
	node *Node,
	actorURL string,
	onRequested func(eventURL string, msg *EventPayload[MsgType]),
	onExecuted func(eventURL string, msg *EventPayload[MsgType]),
) (broadcastRequest func(msg MsgType) error, err error) {
	encoder := NewEncoderProxy[MsgType]()

	if onRequested == nil && onExecuted == nil {
		// add a fake hook so the event url is registered
		// and has a decoder the node has access to
		onRequested = func(eventURL string, msg *EventPayload[MsgType]) {}
	}

	if onExecuted != nil {
		hook := newEventHook(onExecuted)
		hook.encoderProxy = encoder

		node.hooksMu.RLock()
		_, ok := node.hooks[actorURL]
		node.hooksMu.RUnlock()

		node.hooksMu.Lock()
		if !ok {
			node.hooks[actorURL] = map[EventType]*eventHook{
				Executed: hook,
			}
		} else {
			node.hooks[actorURL][Executed] = hook
		}
		node.hooksMu.Unlock()

		if node.subscribed {
			err := node.subscribeTopic(actorURL + "/" + string(Executed))
			if err != nil {
				return nil, err
			}
		}
	}

	if onRequested != nil {
		hook := newEventHook(onRequested)
		hook.encoderProxy = encoder

		node.hooksMu.RLock()
		_, ok := node.hooks[actorURL]
		node.hooksMu.RUnlock()

		node.hooksMu.Lock()
		if !ok {
			node.hooks[actorURL] = map[EventType]*eventHook{
				Request: hook,
			}
		} else {
			node.hooks[actorURL][Request] = hook
		}
		node.hooksMu.Unlock()

		if node.subscribed {
			err := node.subscribeTopic(actorURL + "/" + string(Request))
			if err != nil {
				return nil, err
			}
		}
	}

	return func(msg MsgType) error {
		return node.Broadcast(actorURL+"/"+string(Request), msg)
	}, nil
}

func newEventHook[MsgType any](onEvent OnEventFunc[MsgType]) *eventHook {
	return &eventHook{
		encoderProxy: NewEncoderProxy[MsgType](),
		happened: func(eventURL string, e *EventPayload[any]) error {
			switch d := e.Data.(type) {
			case MsgType:
				onEvent(eventURL, &EventPayload[MsgType]{
					Data:      d,
					DateSent:  e.DateSent,
					Sender:    e.Sender,
					EventType: e.EventType,
				})
				return nil
			default:
				return errors.New("incorrect message type provided to event hook")
			}
		},
	}
}

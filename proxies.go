package wts

import (
	"fmt"
)

type actorProxy struct {
	*encoderProxy
	// act calls the Act method on the proxied actor
	act IndicatorFunc[any]
	// shouldAct calls the ShouldAct method on the proxied actor
	shouldAct IndicatorFunc[any]
}

func newActorProxy[MsgType any](a Actor[MsgType]) *actorProxy {
	return &actorProxy{
		encoderProxy: NewEncoderProxy[MsgType](),
		act:          proxyIndicatorFunc(a.Act, "actorProxy.act() called with incorrect type"),
		shouldAct:    proxyIndicatorFunc(a.ShouldAct, "actorProxy.shouldAct() called with incorrect type"),
	}
}

func proxyIndicatorFunc[MsgType any](
	cb IndicatorFunc[MsgType],
	panicMessage string,
) IndicatorFunc[any] {
	return func(msg *EventPayload[any]) (ok bool) {
		switch msgData := msg.Data.(type) {
		case MsgType:
			return cb(&EventPayload[MsgType]{
				Data:      msgData,
				DateSent:  msg.DateSent,
				EventType: msg.EventType,
				Sender:    msg.Sender,
			})
		default:
			panic(panicMessage)
		}
	}
}

// An encoder proxy proxies encoding for a specific type to generic functions
type encoderProxy struct {
	// Decodes a message using the correct type for the proxied type
	Decode func(bytes []byte) (*EventPayload[any], error)
	// Encodes a message using the correct type for the proxied type
	Encode func(msgData any, eventType EventType, sender string) ([]byte, error)
}

func NewEncoderProxy[MsgType any]() *encoderProxy {
	return &encoderProxy{
		Encode: func(msg any, eventType EventType, sender string) ([]byte, error) {
			switch msg := msg.(type) {
			case MsgType:
				return EncodeMessage(msg, eventType, sender)
			default:
				return nil, fmt.Errorf(
					"encoderProxy.encode: expected type %T but found %T",
					*new(MsgType), msg,
				)
			}
		},
		Decode: func(bytes []byte) (*EventPayload[any], error) {
			payload, err := DecodeMessage[MsgType](bytes)
			if err != nil {
				return nil, err
			}

			return payload.CopyToAny(), nil
		},
	}
}

type emitterProxy struct {
	*encoderProxy
	// dataChannel returns a channel that gets all
	// messages from the proxied emitter
	dataChannel func() <-chan any
}

func newEmitterProxy[MsgType any](e Emitter[MsgType]) *emitterProxy {
	ch := make(chan any)

	go func(ch chan any, ch2 <-chan MsgType) {
		ch <- <-ch2
	}(ch, e.DataEvents())

	return &emitterProxy{
		encoderProxy: NewEncoderProxy[MsgType](),
		dataChannel: func() <-chan any {
			return ch
		},
	}
}

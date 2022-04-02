package wts

import (
	"encoding/json"
	"errors"
	"time"
)

// An EventType is associated with a EventPayload,
// and tells the receiver what type of event
// the payload is
type EventType string

const (
	// A request event is received by the node when
	// another service requested the action to be performed
	//
	// The node consults the actor and performs the action,
	// once the action is complete the node will send an
	// executed event.
	Request EventType = "request"
	// An 'executed' event is sent by the node when the
	// action is performed successfully.
	Executed EventType = "executed"
	// A data event is sent by the node when an emitter
	// emits a data event locally.
	Data EventType = "data"
)

const (
	// websub Content-Type used for event payloads.
	PayloadContentType string = "application/vnd.wts-event-payload.v1+json"
)

var (
	ErrMismatchedTypeHash = errors.New("mismatched type hash while decoding EventPayload")
)

// EventPayload is the message that is sent over websub.
type EventPayload[MsgType any] struct {
	// The event data.
	Data      MsgType   `json:"data"`
	DateSent  time.Time `json:"dateSent"`
	EventType EventType `json:"eventType"`
	Sender    string    `json:"sender"`
}

// CopyToAny creates a new copy of e with the [any] type parameter
func (e *EventPayload[MsgType]) CopyToAny() *EventPayload[any] {
	if e == nil {
		return nil
	}

	var new EventPayload[any]
	new.Data = e.Data
	new.DateSent = e.DateSent
	new.EventType = e.EventType
	new.Sender = e.Sender

	return &new
}

// Encodes a message to JSON
func EncodeMessage[MsgType any](
	msg MsgType,
	eventType EventType,
	sender string,
) ([]byte, error) {
	return json.Marshal(EventPayload[MsgType]{
		Data:      msg,
		DateSent:  time.Now(),
		EventType: eventType,
		Sender:    sender,
	})
}

// Decodes from JSON.
func DecodeMessage[MsgType any](bytes []byte) (*EventPayload[MsgType], error) {
	m := &EventPayload[MsgType]{}
	err := json.Unmarshal(bytes, m)

	if err != nil {
		return nil, err
	}

	return m, nil
}

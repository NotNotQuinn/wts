package wts

// An Emitter is a data source for a Node
// to publish data events about.
type Emitter[MsgType any] interface {
	// Returns a human-readable constant name for this emitter.
	Name() string
	// Returns a channel that passes dataEvents to be published.
	DataEvents() <-chan MsgType
}

// BasicEmitter is a basic implementation of an Emitter.
type BasicEmitter[MsgType any] struct {
	EmitterName string
	DataChannel <-chan MsgType
}

// Returns a human-readable constant name for this emitter.
func (e *BasicEmitter[MsgType]) Name() string {
	return e.EmitterName
}

// Returns a channel that passes dataEvents to be published.
func (e *BasicEmitter[MsgType]) DataEvents() <-chan MsgType {
	return e.DataChannel
}

// NewBasicEmitter creates and returns a new BasicEmitter as Emitter[MsgType]
func NewBasicEmitter[MsgType any](name string, ch <-chan MsgType) Emitter[MsgType] {
	return Emitter[MsgType](&BasicEmitter[MsgType]{
		EmitterName: name,
		DataChannel: ch,
	})
}

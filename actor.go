package wts

// Actor performs an action on behalf of the node,
// which gets requests from other services.
//
// If you would like to trigger the actor programmatically please
// do it through the Node the actor uses, so the proper events can be fired.
type Actor[MsgType any] interface {
	// Returns a human-readable, URL safe name for this actor. (can contain slashes)
	Name() string
	// ShouldAct returns whether the Act method
	// should be called for this message.
	ShouldAct(msg *EventPayload[MsgType]) (ok bool)
	// Act performs an action, and returns whether the
	// action was completed successfully.
	Act(msg *EventPayload[MsgType]) (ok bool)
}

// FuncActor is a basic implementation of an actor.
type FuncActor[MsgType any] struct {
	ActorName     string
	ShouldActFunc IndicatorFunc[MsgType]
	ActFunc       IndicatorFunc[MsgType]
}

// IndicatorFunc indicates something in relation to an event payload.
type IndicatorFunc[MsgType any] func(msg *EventPayload[MsgType]) (ok bool)

// Name returns a human-readable name for this actor.
func (a FuncActor[MsgType]) Name() string {
	return a.ActorName
}

// ShouldAct returns whether the Act method should be called for this message.
func (a FuncActor[MsgType]) ShouldAct(msg *EventPayload[MsgType]) (ok bool) {
	return a.ShouldActFunc(msg)
}

// Act performs an action, and returns whether the
// action was completed successfully.
func (a FuncActor[MsgType]) Act(msg *EventPayload[MsgType]) (ok bool) {
	return a.ActFunc(msg)
}

// NewFuncActor creates a new actor that calls the passed functions for its methods.
func NewFuncActor[MsgType any](
	name string,
	shouldAct IndicatorFunc[MsgType],
	act IndicatorFunc[MsgType],
) Actor[MsgType] {
	return &FuncActor[MsgType]{
		ActorName:     name,
		ShouldActFunc: shouldAct,
		ActFunc:       act,
	}
}

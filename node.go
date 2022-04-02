package wts

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/notnotquinn/go-websub"
)

var log = websub.Logger().With().Caller().Logger()

var (
	// a reserved entity name was provided
	ErrReservedName = errors.New("a reserved entity name was provided")
	// an invalid entity name was provided
	ErrInvalidName = errors.New("an invalid entity name was provided")
	// an actor or emitter name was provided that is already in-use
	ErrUsedName = errors.New("an actor or emitter name was provided that is already in-use")
)

// A node acts as a GenericActor or a GenericEmitter or both.
type Node struct {
	// websub publisher used to publish websub events for communication
	*websub.Publisher
	// websub subscriber used to listen for websub events for communication
	*websub.Subscriber
	// Base URL for the Node
	baseUrl string
	// maps entity URL to actor
	actors map[string]*actorProxy
	// maps entity URL to emitter
	emitters map[string]*emitterProxy
	// maps entity URL to external
	external map[string]*externalProxy
	// subscriptions required for this node to function
	subscriptions []*websub.SubscriberSubscription
	// shared mutex for actors map, emitters map, external map, and subscriptions array
	//
	// could be seperated into 4 mutexes but thats a lot of variables for smol benifit
	mu *sync.RWMutex
	// Used in initialization of publisher only
	pubOptions []websub.PublisherOption
	// Used in initialization of subscriber only
	subOptions []websub.SubscriberOption
	// used to direct http traffic to publisher or subscriber.
	mux *http.ServeMux
	// whether this node is subscribed to the required topics to function properly.
	subscribed bool
}

func (n *Node) BaseUrl() string {
	return n.baseUrl
}

// NewNode creates a new node with the provided options.
func NewNode(baseUrl, hubUrl string, options ...NodeOption) *Node {
	baseUrl = strings.TrimRight(baseUrl, "/")
	n := &Node{
		baseUrl:  baseUrl,
		actors:   make(map[string]*actorProxy),
		emitters: make(map[string]*emitterProxy),
		external: make(map[string]*externalProxy),
		mu:       &sync.RWMutex{},
		pubOptions: []websub.PublisherOption{
			// Used to allow subscribers to subscribe to topic
			// urls we dont publish but are still point to our server
			websub.PublisherAdvertiseInvalidTopics(true),
			// Used to allow us to publish events with topic
			// urls that arent on our server
			websub.PublisherWithPostBodyAsContent(true),
		},
		subOptions: []websub.SubscriberOption{},
		mux:        http.NewServeMux(),
	}

	for _, opt := range options {
		opt(n)
	}

	n.Publisher = websub.NewPublisher(baseUrl+"/", hubUrl, n.pubOptions...)
	n.Subscriber = websub.NewSubscriber(baseUrl+"/_s/", n.subOptions...)
	// unallocate
	n.pubOptions = nil
	n.subOptions = nil

	// "/:actor.ActorName()/request"
	//    - action request
	//    - received by this node (or anyone)
	//    - sent by anyone
	// "/:actor.ActorName()/executed"
	//    - action event (action was executed)
	//    - received by anyone
	//    - sent by this node (or anyone, but they shouldnt!!)
	//                        (authenticate publishers to solve this issue)
	// "/:emitter.EmitterName()/data"
	//    - data events
	//    - sent by this node
	//    - received by anyone
	// these events are ones associated with this node,
	// not necessarily ones published by this node.
	n.mux.Handle("/", n.Publisher)
	// "/_s/*" is for websub subscription callbacks
	n.mux.Handle("/_s/", http.StripPrefix("/_s", n.Subscriber))

	return n
}

func (n *Node) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	n.mux.ServeHTTP(w, r)
}

type NodeOption func(n *Node)

// Defaults to
//
//  []websub.PublisherOption{
//    websub.PAdvertiseInvalidTopics(true),
//    websub.PWithPostBodyAsContent(true),
//  }
//
// Defaults take precedence over options set here, and
// base url is set in relation to the Node's baseUrl.
func WithPublisherOptions(opts ...websub.PublisherOption) NodeOption {
	return func(n *Node) {
		n.pubOptions = append(opts, n.pubOptions...)
	}
}

// Defaults to
//
//  []websub.SubscriberOption{
//  }
//
// Defaults take precedence over options set here, and
// base url is set in relation to the Node's baseUrl.
func WithSubscriberOptions(
	opts ...websub.SubscriberOption,
) NodeOption {
	return func(n *Node) {
		n.subOptions = append(opts, n.subOptions...)
	}
}

// SubscribeAll subscribes to topics required by the node to function.
func (n *Node) SubscribeAll() error {
	if n.subscribed {
		return errors.New("already subscribed")
	}

	n.subscribed = true

	n.mu.RLock()
	for actorUrl := range n.actors {
		err := n.subscribeTopic(actorUrl + "/" + string(Request))
		if err != nil {
			n.mu.RUnlock()
			return err
		}
	}

	n.mu.RUnlock()
	return nil
}

// UnsubscribeALl
func (n *Node) UnsubscribeAll() error {
	if !n.subscribed {
		return errors.New("not subscribed")
	}

	n.subscribed = false

	n.mu.RLock()
	for _, subscription := range n.subscriptions {
		err := n.Unsubscribe(subscription)
		if err != nil {
			n.mu.RUnlock()
			return err
		}
	}

	n.mu.RUnlock()
	return nil
}

// subscribeTopic subscribes to a topic with a random secret
// and the node's callback function
func (n *Node) subscribeTopic(topic string) error {
	secret := make([]byte, 100)
	_, err := rand.Read(secret)
	if err != nil {
		return err
	}

	subscription, err := n.Subscriber.Subscribe(
		topic,
		base64.RawURLEncoding.EncodeToString(secret),
		n.handleSubscription,
	)

	n.mu.Lock()
	n.subscriptions = append(n.subscriptions, subscription)
	n.mu.Unlock()

	return err
}

// handleSubscription receives all events from all subscriptions the node makes.
func (n *Node) handleSubscription(
	sub *websub.SubscriberSubscription,
	contentType string,
	body io.Reader,
) {
	if contentType != PayloadContentType {
		log.Debug().
			Str("content-type", contentType).
			Msg("incorrect payload content-type received from subscription")
		return // ignore
	}

	if !strings.HasPrefix(sub.Topic, n.baseUrl) {
		log.Debug().
			Str("topic", sub.Topic).
			Str("baseUrl", n.baseUrl).
			Msg("entity URL in subscription does not start with node baseUrl")
		return // ignore
	}

	entityUrl, eventType, err := n.parseEventUrl(sub.Topic)
	if err != nil {
		log.Debug().
			AnErr("parsingError", err).
			Str("topic", sub.Topic).
			Msg("invalid entity url as subscribed topic")
		return
	}

	switch eventType {
	case Request:
		encoder, err := n.getEventEncoder(sub.Topic)
		if err != nil {
			log.Err(err).
				Str("topic", sub.Topic).
				Msg("could not get encoder for subscribed topic")
			return
		}

		content, err := io.ReadAll(body)
		if err != nil {
			log.Err(err).
				Msg("could not read subscription content")
			return
		}

		message, err := encoder.Decode(content)
		if err != nil {
			log.Err(err).
				Msg("could not decode subscription content")
			return
		}

		n.mu.RLock()
		actor, exists := n.actors[entityUrl]
		n.mu.RUnlock()
		if !exists {
			// how would this even happen
			log.Error().
				Str("topic", sub.Topic).
				Msg("actor does not exist")
			return
		}

		if actor.shouldAct(message) {
			if actor.act(message) {
				eventUrl := entityUrl + "/" + string(Executed)
				err := n.Broadcast(eventUrl, message.Data)
				if err != nil {
					log.Err(err).
						Str("eventUrl", eventUrl).
						Msg("could not broadcast execution")
					return
				}
			}
		}

		return

	case Executed, Data:
		log.Debug().
			Str("eventType", string(eventType)).
			Msg("unexpected eventType")
		return // ignore

	default:
		log.Debug().
			Str("eventType", string(eventType)).
			Msg("unrecognized eventType")
		return // ignore
	}
}

func (n *Node) parseEventUrl(eventUrl string) (entityUrl string, eventType EventType, err error) {
	parsed, err := url.Parse(eventUrl)
	if err != nil {
		return "", "", fmt.Errorf("invalid event url: %w", err)
	}

	path := strings.Trim(parsed.Path, "/")

	lastSlash := strings.LastIndex(path, "/")
	if lastSlash == -1 {
		return "", "", errors.New("event url must contain one internal slash")
	}

	entityPath, eventType := path[:lastSlash], EventType(path[lastSlash+1:])

	parsed.Path = entityPath
	entityUrl = parsed.String()

	switch eventType {
	case Executed, Data, Request:
		return

	default:
		return "", "", fmt.Errorf("event url contains unrecognized eventType: %q", eventType)
	}
}

func (n *Node) Broadcast(eventUrl string, msgData any) error {
	content, err := n.encode(eventUrl, msgData)
	if err != nil {
		return err
	}

	err = n.Publish(eventUrl, PayloadContentType, content)
	if err != nil {
		return err
	}

	return nil
}

// getEventEncoder gets the encoder proxy for a specific event
func (n *Node) getEventEncoder(eventUrl string) (*encoderProxy, error) {
	entityUrl, eventType, err := n.parseEventUrl(eventUrl)
	if err != nil {
		return nil, err
	}

	switch eventType {
	case Request, Executed:
		var encoder *encoderProxy
		n.mu.RLock()
		actor, exists := n.actors[entityUrl]
		n.mu.RUnlock()
		if exists {
			encoder = actor.encoderProxy
		} else {
			n.mu.RLock()
			external, exists := n.external[entityUrl]
			n.mu.RUnlock()
			if exists && external.actor != nil {
				encoder = external.actor
			} else {
				return nil, errors.New("actor does not exist internally or externally")
			}
		}

		return encoder, nil

	case Data:
		var encoder *encoderProxy
		n.mu.RLock()
		emitter, exists := n.emitters[entityUrl]
		n.mu.RUnlock()
		if exists {
			encoder = emitter.encoderProxy
		} else {
			n.mu.RLock()
			external, exists := n.external[entityUrl]
			n.mu.RUnlock()
			if exists && external.emitter != nil {
				encoder = external.emitter
			} else {
				return nil, errors.New("emitter does not exist internally or externally")
			}
		}

		return encoder, nil

	default:
		return nil, errors.New("unrecognized event type")
	}
}

// Encode encodes a message according to the event's type
func (n *Node) encode(eventUrl string, msgData any) ([]byte, error) {
	_, eventType, err := n.parseEventUrl(eventUrl)
	if err != nil {
		return nil, err
	}

	encoder, err := n.getEventEncoder(eventUrl)
	if err != nil {
		return nil, err
	}

	content, err := encoder.Encode(msgData, eventType, n.baseUrl)
	if err != nil {
		return nil, err
	}

	return content, nil
}

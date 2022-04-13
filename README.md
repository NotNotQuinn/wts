# Websub Transport System

Websub transport system (WTS) is a janky data communication library written in go. It transports go structs over websub as JSON objects, along with some meta-data, such as date/time sent, event type, and sender.

### Importing

Go version 1.18+ is required.
```bash
go get github.com/notnotquinn/wts
```

## What does it do?

WTS sends go structs as JSON over websub, and has 3 major parts: Node, the Actor interface, and the Emitter interface.

A node is a host for Actors and Emitters, and is the unit of communication. Nodes communicate with each other based on the needs of their Actors and Emitters.

An Emitter just sends events, and other nodes can subscribe to those "data" events.

An actor listens to action "request"s, and calls a callback and sends an "executed" event when the requests are received. When the callback is not performed successfully (or the request is denied), the executed event is not sent.

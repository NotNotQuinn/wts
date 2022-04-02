package manager

import (
	"os"

	"github.com/notnotquinn/wts"
	"gopkg.in/yaml.v3"
)

// Config is the configuration for a Manager
type Config struct {
	HubURL  string          `yaml:"hubURL"`
	HubPort string          `yaml:"hubPort"`
	Rules   map[string]Rule `yaml:"rules"`
	Scripts Scripts         `yaml:"scripts"`
}

// Rule is a single logical rule for
// a manager to check when it receives events
type Rule struct {
	Triggers []Trigger `yaml:"triggers"`
	Action   Action    `yaml:"action"`
}

// Trigger specifies a single trigger for a rule
type Trigger struct {
	Event ReceiveEvent `yaml:"event"`
}

// Action specifies a single action to take once
// a rule is triggered
type Action struct {
	Event SendEvent `yaml:"event"`
}

// SendEvent is an event that is meant to be sent
type SendEvent struct {
	Event        `yaml:"event,inline"`
	OnSendScript string `yaml:"onSendScript"`
}

// ReceiveEvent is an event that is meant to be received
type ReceiveEvent struct {
	Event           `yaml:"event,inline"`
	OnReceiveScript string `yaml:"onReceiveScript"`
}

// Event specifies an event URL and Event Type.
// Differentiating between data/request/execution of
// the same resource
type Event struct {
	Type wts.EventType `yaml:"type"`
	URL  string        `yaml:"url"`
}

type Scripts struct {
	Enabled bool   `yaml:"enabled"`
	Init    string `yaml:"init"`
}

func (c *Config) Load(configFile string) error {
	content, err := os.ReadFile(configFile)
	if err != nil {
		return err
	}
	return yaml.Unmarshal(content, c)
}

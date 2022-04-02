/*
 */
package manager

import (
	"fmt"

	"github.com/notnotquinn/go-websub"
	"rogchap.com/v8go"
)

// Manager acts as the Hub for multiple Nodes that communicate to eachother,
// and can intercept all communications and events broadcasted between the nodes
// to trigger other events.
type Manager struct {
	*websub.Hub
	Config *Config
	v8Ctx  *v8go.Context
}

func NewManager(configFile string) (*Manager, error) {
	conf := &Config{}
	err := conf.Load(configFile)
	if err != nil {
		return nil, err
	}

	var ctx *v8go.Context
	if conf.Scripts.Enabled && conf.Scripts.Init != "" {
		ctx, err = newV8Context()
		if err != nil {
			return nil, err
		}

		_, err := ctx.RunScript(conf.Scripts.Init, "config.scripts.init")
		if err != nil {
			e := err.(*v8go.JSError)
			fmt.Printf("JSError: %+v\n", e)
			return nil, e
		}
	}

	return &Manager{
		Hub:    websub.NewHub(conf.HubURL),
		Config: conf,
		v8Ctx:  ctx,
	}, nil
}

func init() {
	fmt.Println("V8: Initialized!")
	v8go.SetFlags("--use_strict")
}

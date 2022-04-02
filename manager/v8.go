package manager

import (
	"os"

	"go.kuoruan.net/v8go-polyfills/console"
	"rogchap.com/v8go"
)

// v8 is used to run JS scripts from the .yaml config file

// returns a new V8 context with a console
func newV8Context() (*v8go.Context, error) {
	ctx := v8go.NewContext()

	err := console.InjectMultipleTo(ctx,
		console.NewConsole(console.WithOutput(os.Stderr), console.WithMethodName("error")),
		console.NewConsole(console.WithOutput(os.Stdout), console.WithMethodName("log")),
	)
	if err != nil {
		return nil, err
	}

	return ctx, nil
}

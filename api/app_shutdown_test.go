package api

import (
	"testing"

	"gitlab.bbdev.team/vh/pay/orders/common"
)

// Shutdown is reached from the fatal paths as well as from server.go's defer, so
// it runs with whatever Initialize managed to build. A fatal in initSentry or
// initEventEmitter leaves both fields nil, and an unguarded Close on a nil
// interface panics — turning a stated error into a crash.
func TestShutdownSurvivesAPartialInitialize(t *testing.T) {
	new(App).Shutdown()
}

// The emitter is always built. It used to be built only when NatsUrl was set,
// which left a nil interface that the first emitEvent would call Emit on —
// reachable by following .env_example, which ships NATS_URL empty.
func TestTheEmitterIsBuiltWithoutNats(t *testing.T) {
	saved := *common.Config
	t.Cleanup(func() { *common.Config = saved })
	common.Config.NatsUrl = ""

	var app App
	app.initEventEmitter()

	if app.eventEmitter == nil {
		t.Fatal("no emitter without NATS: the first emitEvent would panic on a nil interface")
	}
}

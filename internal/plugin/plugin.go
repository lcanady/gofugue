// Package plugin defines the GoFugue plugin interface and built-in registry.
// Plugins may be compiled in (register via init()) or loaded dynamically via
// plugin.Open (Go plugin mechanism, Unix only).
package plugin

import (
	"context"

	"github.com/kumakun/gofugue/internal/bus"
)

// API is the surface exposed to plugins — everything a plugin needs to interact
// with GoFugue without importing internal packages directly.
type API interface {
	// Bus returns the central event bus for subscribing and publishing.
	Bus() *bus.Bus

	// Send sends text to the named world (empty = foreground world).
	Send(world, text string) error

	// Echo prints text to the local output pane (not sent to server).
	Echo(text string)

	// RegisterCmd registers a new /command handled by this plugin.
	RegisterCmd(name string, handler CmdHandler)

	// Setvar / Getvar manage the global variable namespace.
	Setvar(name, value string)
	Getvar(name string) (string, bool)
}

// CmdHandler is called when the user (or a macro) invokes a registered /command.
type CmdHandler func(api API, args string) error

// Plugin is implemented by every GoFugue plugin.
type Plugin interface {
	// Name returns the plugin's unique identifier.
	Name() string

	// Init is called once after the plugin is loaded, before any worlds connect.
	Init(ctx context.Context, api API) error

	// Shutdown is called on clean exit or plugin unload.
	Shutdown() error
}

// Registry holds all compiled-in plugins.
var registry []Plugin

// Register adds a plugin to the compiled-in registry.
// Call from an init() function in the plugin package.
func Register(p Plugin) {
	registry = append(registry, p)
}

// All returns all registered compiled-in plugins.
func All() []Plugin {
	return registry
}

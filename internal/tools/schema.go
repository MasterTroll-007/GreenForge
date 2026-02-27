package tools

// Note: ToolDef has an unexported handler field for built-in tools.
// We need to extend the struct definition. Since Go doesn't support
// adding fields after definition, we use a separate map.

import (
	"sync"
)

var (
	builtinHandlers   = make(map[string]BuiltinHandler)
	builtinHandlersMu sync.RWMutex
)

// SetBuiltinHandler registers a handler for a built-in tool name.
func SetBuiltinHandler(name string, handler BuiltinHandler) {
	builtinHandlersMu.Lock()
	defer builtinHandlersMu.Unlock()
	builtinHandlers[name] = handler
}

// GetBuiltinHandler retrieves a registered built-in handler.
func GetBuiltinHandler(name string) (BuiltinHandler, bool) {
	builtinHandlersMu.RLock()
	defer builtinHandlersMu.RUnlock()
	h, ok := builtinHandlers[name]
	return h, ok
}

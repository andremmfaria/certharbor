package delivery

import "fmt"

// Registry is the compiled-in set of available Target implementations,
// keyed by their Name(). plans/distribution.md Section 7: adding a target
// means implementing Target and registering it here, no reconciler change.
type Registry struct {
	targets map[string]Target
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{targets: make(map[string]Target)}
}

// Register adds a target implementation under its own Name(). Registering
// the same name twice is a programmer error and panics at startup rather
// than silently shadowing one implementation with another.
func (r *Registry) Register(t Target) {
	name := t.Name()
	if _, exists := r.targets[name]; exists {
		panic(fmt.Sprintf("delivery: target %q already registered", name))
	}
	r.targets[name] = t
}

// Get returns the target implementation for a delivery_targets.type value.
func (r *Registry) Get(typeName string) (Target, bool) {
	t, ok := r.targets[typeName]
	return t, ok
}

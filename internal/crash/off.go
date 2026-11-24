//go:build !crash

package crash

// Step marks a step at which kiln changes durable state. In kiln's own build it does nothing.
func Step(_ string) {}

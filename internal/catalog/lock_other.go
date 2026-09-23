//go:build !(linux || darwin)

package catalog

// On platforms without an advisory-lock primitive, locking is a no-op: concurrent
// charon invocations there are not serialized. This keeps the Catalog API uniform
// across build targets.
func (c *Catalog) openLock() error      { return nil }
func (c *Catalog) acquireOSLock() error { return nil }
func (c *Catalog) releaseOSLock() error { return nil }

package store

// SetHook installs a test hook (see hook) and returns the function that
// removes it.
func SetHook(f func(point, arg string)) func() {
	testHook.Store(&f)
	return func() { testHook.Store(nil) }
}

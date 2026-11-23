//go:build !darwin

package computer

func system() Desktop { return nil }

// SetPermissions does nothing where computer use does not run.
func SetPermissions(func() []string) {}

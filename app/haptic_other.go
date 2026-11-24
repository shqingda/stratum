//go:build !darwin

package app

type hapticPattern int

const (
	hapticAlignment hapticPattern = 1
)

func performHaptic(hapticPattern) {}

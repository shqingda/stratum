//go:build !darwin

package app

type hapticPattern int

const (
	hapticAlignment   hapticPattern = 1
	hapticLevelChange hapticPattern = 2
)

func performHaptic(hapticPattern) {}

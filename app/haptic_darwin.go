//go:build darwin

package app

import (
	"sync"
	"testing"

	"github.com/ebitengine/purego/objc"
)

type hapticPattern int

const (
	hapticAlignment hapticPattern = 1 // NSHapticFeedbackPatternAlignment
)

const hapticTimeNow = 1 // NSHapticFeedbackPerformanceTimeNow

var (
	hapticOnce          sync.Once
	selDefaultPerformer objc.SEL
	selPerformFeedback  objc.SEL
)

func performHaptic(pattern hapticPattern) {
	if testing.Testing() {
		return
	}
	hapticOnce.Do(func() {
		selDefaultPerformer = objc.RegisterName("defaultPerformer")
		selPerformFeedback = objc.RegisterName("performFeedbackPattern:performanceTime:")
	})
	cls := objc.ID(objc.GetClass("NSHapticFeedbackManager"))
	if cls == 0 {
		return
	}
	performer := cls.Send(selDefaultPerformer)
	if performer == 0 {
		return
	}
	performer.Send(selPerformFeedback, int(pattern), hapticTimeNow)
}

package client

import (
	"os"
	"testing"

	"github.com/egoist/mygo/i18n"
)

// TestMain has the tests read Chinese, whatever language this machine is in.
func TestMain(m *testing.M) {
	i18n.SetLocaleFunc(func() string { return "zh-Hans" })
	os.Exit(m.Run())
}

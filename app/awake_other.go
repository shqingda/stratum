//go:build !darwin

package app

type sleepGuard struct{}

func (*sleepGuard) Set(bool) {}

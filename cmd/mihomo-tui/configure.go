package main

import (
	"errors"
	"mihomo-tui/internal/state"
)

var errPortsSame = errors.New("API and proxy ports must differ")

func configurePorts(c *state.Settings, mode string, mixed, api int, check func(int) error) error {
	if mode == "system" && c.InstallMode != "system" {
		if mixed == 0 {
			mixed = 7890
		}
		if api == 0 {
			api = 9090
		}
	}
	nextMixed, nextAPI := c.MixedPort, c.APIPort
	if mixed != 0 {
		if err := check(mixed); err != nil {
			return err
		}
		nextMixed = mixed
	}
	if api != 0 {
		if err := check(api); err != nil {
			return err
		}
		nextAPI = api
	}
	if nextMixed == nextAPI {
		return errPortsSame
	}
	c.MixedPort, c.APIPort = nextMixed, nextAPI
	return nil
}

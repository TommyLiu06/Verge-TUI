package main

import (
	"errors"
	"net"
	"strconv"
	"verge-tui/internal/state"
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
		if c.ControllerEnabled {
			if err := check(api); err != nil {
				return err
			}
		}
		nextAPI = api
	}
	if nextMixed == nextAPI {
		return errPortsSame
	}
	if c.ControllerAddress != "" && nextAPI != c.APIPort {
		host, _, err := net.SplitHostPort(c.ControllerAddress)
		if err != nil {
			return err
		}
		c.ControllerAddress = net.JoinHostPort(host, strconv.Itoa(nextAPI))
	}
	c.MixedPort, c.APIPort = nextMixed, nextAPI
	return nil
}

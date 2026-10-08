package testutil

import (
	"context"
	"errors"
	"strings"
)

// Desktop models dconf writes and observed gsettings values without host access.
type Desktop struct {
	Original, Payload string
	FailNextLoad      bool
	Loads             int
}

func (d *Desktop) Run(_ context.Context, name string, args []string, input string) (string, error) {
	if name == "dconf" {
		switch args[0] {
		case "dump":
			if d.Payload != "" {
				return strings.TrimSuffix(d.Payload, "\n"), nil
			}
			return strings.TrimSuffix(d.Original, "\n"), nil
		case "reset":
			d.Payload = ""
		case "load":
			d.Loads++
			d.Payload = input
			if d.FailNextLoad {
				d.FailNextLoad = false
				return "", errors.New("simulated partial dconf write")
			}
		}
		return "", nil
	}
	if name == "gsettings" {
		for _, line := range strings.Split(d.Payload, "\n") {
			if strings.HasPrefix(line, args[2]+"=") {
				return strings.TrimPrefix(line, args[2]+"="), nil
			}
		}
		if args[2] == "mode" {
			return "'none'", nil
		}
		return "", nil
	}
	return "", errors.New("unexpected desktop command")
}

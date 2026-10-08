package main

import (
	"errors"
	"reflect"
	"testing"

	"mihomo-tui/internal/state"
)

func TestSystemPortsDefaultAndPreserveUpdates(t *testing.T) {
	for _, item := range []struct {
		previous, next     string
		mixed, api         int
		wantMixed, wantAPI int
		checked            []int
	}{
		{"user", "system", 0, 0, 7890, 9090, []int{7890, 9090}},
		{"system", "system", 0, 0, 23456, 34567, nil},
		{"user", "user", 0, 0, 23456, 34567, nil},
		{"user", "system", 8123, 9123, 8123, 9123, []int{8123, 9123}},
	} {
		c := state.Settings{InstallMode: item.previous, MixedPort: 23456, APIPort: 34567}
		var checked []int
		err := configurePorts(&c, item.next, item.mixed, item.api, func(port int) error { checked = append(checked, port); return nil })
		if err != nil || c.MixedPort != item.wantMixed || c.APIPort != item.wantAPI || !reflect.DeepEqual(checked, item.checked) {
			t.Fatalf("wrong default or preserved ports: %+v %v %v", c, checked, err)
		}
	}
}

func TestPortConflictLeavesOriginalConfiguration(t *testing.T) {
	c := state.Settings{InstallMode: "user", MixedPort: 23456, APIPort: 34567}
	err := configurePorts(&c, "system", 0, 0, func(port int) error {
		if port == 9090 {
			return errors.New("API port is occupied")
		}
		return nil
	})
	if err == nil || c.MixedPort != 23456 || c.APIPort != 34567 {
		t.Fatal("failed port check partially changed settings")
	}
	if err := configurePorts(&c, "system", 7890, 7890, func(int) error { return nil }); !errors.Is(err, errPortsSame) {
		t.Fatal("duplicate ports accepted")
	}
}

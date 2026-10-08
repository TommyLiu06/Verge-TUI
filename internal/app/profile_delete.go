package app

import (
	"context"
	"errors"
	"fmt"
	"os"

	"verge-tui/internal/state"
)

func (b *Backend) deleteProfile(ctx context.Context, id string) error {
	c, err := b.Store.Read()
	if err != nil {
		return err
	}
	if _, found := state.Find(c, id); !found {
		return errors.New("profile does not exist")
	}
	path, err := b.Store.ProfilePath(id)
	if err != nil {
		return err
	}
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
		return errors.New("profile YAML must be a regular file")
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	remove := func(c *state.Settings) error {
		for i, p := range c.Profiles {
			if p.ID == id {
				c.Profiles = append(c.Profiles[:i], c.Profiles[i+1:]...)
				delete(c.HomeGroup, id)
				delete(c.Engine.DNS, id)
				if c.Current == id {
					c.Current = ""
				}
				return nil
			}
		}
		return errors.New("profile was already removed")
	}
	if c.Current == id {
		// Validate and reload the built-in DIRECT configuration before removing
		// the YAML, keeping the old profile intact if the switch fails.
		err = b.Manager.Apply(ctx, remove)
	} else {
		err = b.Store.Update(remove)
	}
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("profile removed, but YAML cleanup failed: %w", err)
	}
	return nil
}

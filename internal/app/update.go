package app

import (
	"errors"

	"verge-tui/internal/updater"
)

func (b *Backend) saveUpdateOptions(fields map[string]string) error {
	if b.Remote != nil {
		return errors.New("system installation update settings require administrator authorization")
	}
	u := updater.Updater{Store: b.Store, Version: b.Version}
	s, err := u.Status()
	if err != nil {
		return err
	}
	if !s.Supported {
		return errors.New("application updates require an installed offline package")
	}
	return u.ModifyOptions(func(options *updater.Options) error {
		next, err := options.WithChanges(fields)
		if err == nil {
			*options = next
		}
		return err
	})
}

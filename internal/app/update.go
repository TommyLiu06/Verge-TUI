package app

import (
	"errors"
	"strconv"

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
		for key, value := range fields {
			n, err := strconv.Atoi(value)
			if err != nil {
				return errors.New("update intervals must be whole minutes")
			}
			switch key {
			case "check-minutes":
				options.CheckMinutes = n
			case "idle-minutes":
				options.IdleMinutes = n
			default:
				return errors.New("unknown update setting")
			}
		}
		return nil
	})
}

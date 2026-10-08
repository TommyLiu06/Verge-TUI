package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"verge-tui/internal/app"
	"verge-tui/internal/shellenv"
	"verge-tui/internal/state"
	"verge-tui/internal/tui"
)

func sharedBackend(root string) (*app.Backend, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	s := &state.Store{Root: abs}
	if _, err := s.Read(); !os.IsPermission(err) {
		return nil, nil
	}
	c, err := shellenv.ReadFile(shellenv.FilePath(s))
	if err != nil {
		return nil, err
	}
	if c.InstallMode != "system" {
		return nil, errors.New("this user's private installation is not accessible")
	}
	return app.NewRemote(s, version), nil
}

func runShared(args []string, b *app.Backend) error {
	if len(args) == 0 {
		args = []string{"tui"}
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	switch args[0] {
	case "tui":
		return tui.Run(ctx, b)
	case "env":
		c, err := shellenv.ReadFile(shellenv.FilePath(b.Store))
		if err != nil {
			return err
		}
		fmt.Print(shellenv.Exports(c))
		return nil
	case "preview":
		f := flag.NewFlagSet("preview", flag.ContinueOnError)
		page := f.String("page", "home", "home, proxies, profiles or settings")
		width, height := f.Int("width", 120, "terminal width"), f.Int("height", 42, "terminal height")
		if err := f.Parse(args[1:]); err != nil {
			return err
		}
		if *width < 1 || *height < 1 || *width > 500 || *height > 500 {
			return errors.New("preview dimensions must be between 1 and 500")
		}
		v, err := b.Load(ctx)
		if err != nil {
			return err
		}
		m := tui.New(v)
		switch *page {
		case "home":
		case "proxies":
			m.Tab = 1
		case "profiles":
			m.Tab = 2
		case "settings":
			m.Tab = 3
		default:
			return errors.New("unknown preview page")
		}
		fmt.Print(m.Render(*width, *height).Plain())
		return nil
	case "status":
		v, err := b.Load(ctx)
		if err != nil {
			return err
		}
		data, err := json.Marshal(struct {
			InstallMode, Mode, Core, Profile string
			MixedPort, APIPort               int
		}{v.Settings.InstallMode, v.Settings.Mode, v.Settings.Core, v.Settings.Current, v.Settings.MixedPort, v.Settings.APIPort})
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	case "core":
		if len(args) < 2 {
			return errors.New("usage: core install [TAG] | list | use ID")
		}
		switch args[1] {
		case "import":
			if len(args) != 4 {
				return errors.New("usage: core import ID FILE")
			}
			if err := b.Remote.ImportCore(ctx, args[2], args[3]); err != nil {
				return err
			}
			fmt.Println("Installed", args[2])
			return nil
		case "install":
			tag := "latest"
			if len(args) > 2 {
				tag = args[2]
			}
			return b.Execute(ctx, app.Action{Kind: "core-install", Value: tag})
		case "use":
			if len(args) != 3 {
				return errors.New("usage: core use ID")
			}
			return b.Execute(ctx, app.Action{Kind: "core-use", ID: args[2]})
		case "list":
			v, err := b.Load(ctx)
			if err != nil {
				return err
			}
			for _, item := range v.Cores {
				mark := " "
				if item.ID == v.Settings.Core {
					mark = "*"
				}
				fmt.Printf("%s %s  %s\n", mark, item.ID, item.Version)
			}
			return nil
		}
	case "profile":
		if len(args) < 2 {
			return errors.New("usage: profile import URL | new NAME [YAML] | list | select ID | delete ID")
		}
		switch args[1] {
		case "import":
			if len(args) != 3 {
				return errors.New("usage: profile import URL")
			}
			return b.Execute(ctx, app.Action{Kind: "profile-import", Profile: state.Profile{URL: args[2]}})
		case "new":
			if len(args) < 3 || len(args) > 4 {
				return errors.New("usage: profile new NAME [YAML]")
			}
			file := ""
			if len(args) == 4 {
				file = args[3]
			}
			return b.Execute(ctx, app.Action{Kind: "profile-new", Profile: state.Profile{Type: "local", Name: args[2]}, File: file})
		case "select", "delete":
			if len(args) != 3 {
				return errors.New("usage: profile select|delete ID")
			}
			return b.Execute(ctx, app.Action{Kind: "profile-" + args[1], ID: args[2]})
		case "list":
			v, err := b.Load(ctx)
			if err != nil {
				return err
			}
			for _, p := range v.Settings.Profiles {
				mark := " "
				if p.ID == v.Settings.Current {
					mark = "*"
				}
				fmt.Printf("%s %s  %s (%s)\n", mark, p.ID, p.Name, p.Type)
			}
			return nil
		}
	}
	return errors.New("service installation, removal and start/stop require root; use the TUI for shared configuration")
}

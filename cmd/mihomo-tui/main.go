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

	"mihomo-tui/internal/app"
	"mihomo-tui/internal/core"
	"mihomo-tui/internal/desktop"
	"mihomo-tui/internal/geodata"
	"mihomo-tui/internal/installation"
	"mihomo-tui/internal/profile"
	"mihomo-tui/internal/service"
	"mihomo-tui/internal/shellenv"
	"mihomo-tui/internal/state"
	"mihomo-tui/internal/tui"
)

const version = "0.1.6"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
func run(args []string) error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	flags := flag.NewFlagSet("mihomo-tui", flag.ContinueOnError)
	root := flags.String("data-dir", filepath.Join(filepath.Dir(executable), "data"), "application-owned data directory")
	flags.Usage = func() {
		fmt.Println("mihomo-tui [--data-dir DIR] [command]\n\nCommands: tui (default), core install|import|list|use, profile import|new|list|select, run, env, env-file, shell install|remove, proxy-status, service, configure, status, preview, version, prepare-uninstall\n\nGo and a system-wide mihomo installation are not required.")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return err
	}
	args = flags.Args()
	if len(args) > 0 && args[0] == "version" {
		fmt.Println(version)
		return nil
	}
	// Commands that only need the shared environment or this user's rc must
	// never initialize/chmod the administrator's private data directory.
	// Bundle installation helpers must not initialize or rewrite existing data
	// while inspecting an installation before the user's confirmation.
	if len(args) > 0 && args[0] == "installation" {
		if len(args) < 2 {
			return errors.New("usage: installation inspect|update --prefix DIR [--rc-file FILE]")
		}
		f := flag.NewFlagSet("installation", flag.ContinueOnError)
		prefix := f.String("prefix", "", "existing installation directory")
		rc := f.String("rc-file", "", "shell rc to refresh")
		source := f.String("source-dir", "", "geography database source directory")
		binary := f.String("core-file", "", "core binary used to verify geography data")
		mixed := f.Int("mixed-port", 0, "proxy port to check")
		api := f.Int("api-port", 0, "API port to check")
		if err := f.Parse(args[2:]); err != nil {
			return err
		}
		switch args[1] {
		case "check-ports":
			if *mixed != 0 && *mixed == *api {
				return errPortsSame
			}
			for _, port := range []int{*mixed, *api} {
				if port != 0 {
					if err := state.CheckPort(port); err != nil {
						return err
					}
				}
			}
			return nil
		case "check-geodata":
			return geodata.Verify(context.Background(), *source, *binary)
		case "seed-geodata":
			if !filepath.IsAbs(*prefix) {
				return errors.New("installation prefix must be absolute")
			}
			bundle := filepath.Dir(filepath.Dir(executable))
			return geodata.Install(context.Background(), filepath.Join(bundle, "geodata"), filepath.Join(*prefix, "data"), filepath.Join(bundle, "cores", "mihomo"))
		case "inspect":
			x, err := installation.Inspect(*prefix)
			if err != nil {
				return err
			}
			kind := "installed"
			if x.Retained {
				kind = "retained"
			}
			fmt.Printf("%s\n%s\n%s\n%s\n%s\n%d\n%d\n", x.Mode, x.Unit, x.RC, x.Core, kind, x.MixedPort, x.APIPort)
			return nil
		case "update":
			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()
			return installation.Update(ctx, filepath.Dir(filepath.Dir(executable)), *prefix, *rc)
		default:
			return errors.New("unknown installation helper")
		}
	}
	if len(args) > 0 && args[0] == "proxy-status" {
		abs, err := filepath.Abs(*root)
		if err != nil {
			return err
		}
		out, err := shellenv.Status(context.Background(), &state.Store{Root: abs}, os.Environ())
		if err != nil {
			return err
		}
		fmt.Print(out)
		return nil
	}
	if len(args) > 0 && args[0] == "shell" {
		abs, err := filepath.Abs(*root)
		if err != nil {
			return err
		}
		s := &state.Store{Root: abs}
		if len(args) < 2 {
			return errors.New("usage: shell install|remove [--rc-file FILE] | shell remove --all")
		}
		f := flag.NewFlagSet("shell", flag.ContinueOnError)
		rc := f.String("rc-file", "", "shell rc path (defaults to the current shell)")
		all := f.Bool("all", false, "remove all registered users' functions (uninstallation)")
		if err := f.Parse(args[2:]); err != nil {
			return err
		}
		switch args[1] {
		case "remove":
			if *rc != "" {
				return errors.New("shell remove uses this user's registered rc; --rc-file is only valid for shell install")
			}
			if *all {
				return shellenv.RemoveAll(s, executable)
			}
			return shellenv.Remove(s)
		case "install":
			if *all {
				return errors.New("--all is only valid for shell remove")
			}
			if *rc == "" {
				*rc, err = shellenv.CurrentRC()
				if err != nil {
					return err
				}
			}
			path, err := shellenv.Register(s, executable, *rc)
			if err != nil {
				return err
			}
			fmt.Printf("Shell functions: mihomo_on, mihomo_off, mihomo_status\nLoad in this shell: . %s\n", shellenv.Quote(path))
			return nil
		default:
			return errors.New("unknown shell command")
		}
	}
	if remote, err := sharedBackend(*root); err != nil {
		return err
	} else if remote != nil && len(args) > 0 && args[0] == "desktop-restore" {
		defer remote.Close(context.Background())
		return desktop.RestoreUser(context.Background(), remote.Store)
	} else if remote != nil {
		defer remote.Close(context.Background())
		return runShared(args, remote)
	}
	s, err := state.Open(*root)
	if err != nil {
		return err
	}
	m := &core.Manager{Store: s}
	ctx := context.Background()
	if len(args) == 0 {
		args = []string{"tui"}
	}
	switch args[0] {
	case "env-file":
		return shellenv.Write(s)
	case "prepare-uninstall":
		if err := app.New(s, version).PrepareUninstall(ctx); err != nil {
			return err
		}
		return desktop.RestoreAll(s, executable)
	case "desktop-restore":
		return desktop.RestoreUser(ctx, s)
	case "tui":
		ctx, cancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
		defer cancel()
		return tui.Run(ctx, app.New(s, version))
	case "preview":
		f := flag.NewFlagSet("preview", flag.ContinueOnError)
		page := f.String("page", "home", "home, proxies or profiles")
		width := f.Int("width", 120, "terminal width")
		height := f.Int("height", 42, "terminal height")
		if err := f.Parse(args[1:]); err != nil {
			return err
		}
		if *width < 1 || *height < 1 || *width > 500 || *height > 500 {
			return errors.New("preview dimensions must be between 1 and 500")
		}
		v, err := app.New(s, version).Load(ctx)
		if err != nil {
			return err
		}
		model := tui.New(v)
		switch *page {
		case "home":
			model.Tab = 0
		case "proxies":
			model.Tab = 1
		case "profiles":
			model.Tab = 2
		default:
			return errors.New("unknown preview page")
		}
		fmt.Print(model.Render(*width, *height).Plain())
		return nil
	case "core":
		if len(args) < 2 {
			return errors.New("usage: core install [TAG] | import ID FILE | list | use ID")
		}
		switch args[1] {
		case "list":
			installed, err := core.List(s)
			if err != nil {
				return err
			}
			c, _ := s.Read()
			for _, item := range installed {
				mark := " "
				if item.ID == c.Core {
					mark = "*"
				}
				fmt.Printf("%s %s  %s\n", mark, item.ID, item.Version)
			}
			return nil
		case "install":
			tag := "latest"
			if len(args) > 2 {
				tag = args[2]
			}
			item, err := core.Download(ctx, s, tag)
			if err != nil {
				return err
			}
			c, _ := s.Read()
			if c.Core == "" {
				if err := m.Use(ctx, item.ID); err != nil {
					return err
				}
			}
			fmt.Println("Installed", item.ID)
			return nil
		case "import":
			if len(args) != 4 {
				return errors.New("usage: core import ID FILE")
			}
			item, err := core.Import(ctx, s, args[2], args[3])
			if err != nil {
				return err
			}
			c, _ := s.Read()
			if c.Core == "" {
				if err := m.Use(ctx, item.ID); err != nil {
					return err
				}
			}
			fmt.Println("Installed", item.ID)
			return nil
		case "use":
			if len(args) != 3 {
				return errors.New("usage: core use ID")
			}
			return m.Use(ctx, args[2])
		}
	case "profile":
		if len(args) < 2 {
			return errors.New("usage: profile import URL | new NAME [YAML] | list | select ID")
		}
		switch args[1] {
		case "import":
			if len(args) != 3 {
				return errors.New("usage: profile import URL")
			}
			p, err := profile.Remote(ctx, s, state.Profile{URL: args[2]})
			if err != nil {
				return err
			}
			fmt.Println(p.ID, p.Name)
			return nil
		case "new":
			if len(args) < 3 || len(args) > 4 {
				return errors.New("usage: profile new NAME [YAML]")
			}
			path := ""
			if len(args) == 4 {
				path = args[3]
			}
			p, err := profile.Local(s, args[2], "", path)
			if err != nil {
				return err
			}
			fmt.Println(p.ID, p.Name)
			return nil
		case "list":
			c, err := s.Read()
			if err != nil {
				return err
			}
			for _, p := range c.Profiles {
				mark := " "
				if p.ID == c.Current {
					mark = "*"
				}
				fmt.Printf("%s %s  %s (%s)\n", mark, p.ID, p.Name, p.Type)
			}
			return nil
		case "select":
			if len(args) != 3 {
				return errors.New("usage: profile select ID")
			}
			return m.Apply(ctx, func(c *state.Settings) error {
				if _, ok := state.Find(*c, args[2]); !ok {
					return errors.New("profile does not exist")
				}
				c.Current = args[2]
				return nil
			})
		}
	case "run":
		ctx, cancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
		defer cancel()
		return runManaged(ctx, m, s)
	case "env":
		c, err := s.Read()
		if err != nil {
			return err
		}
		fmt.Print(shellenv.Exports(c))
		return nil
	case "service":
		f := flag.NewFlagSet("service", flag.ContinueOnError)
		mode := f.String("mode", "user", "user or system")
		if err := f.Parse(args[1:]); err != nil {
			return err
		}
		unit, err := service.Unit(executable, s.Root, *mode)
		if err != nil {
			return err
		}
		fmt.Print(unit)
		return nil
	case "configure":
		f := flag.NewFlagSet("configure", flag.ContinueOnError)
		mode := f.String("install-mode", "user", "user or system")
		mixed := f.Int("mixed-port", 0, "mixed port")
		api := f.Int("api-port", 0, "API port")
		if err := f.Parse(args[1:]); err != nil {
			return err
		}
		if *mode != "user" && *mode != "system" {
			return errors.New("mode must be user or system")
		}
		if *mode == "system" && os.Geteuid() != 0 {
			return errors.New("system mode configuration requires root")
		}
		err := s.Update(func(c *state.Settings) error {
			running, err := core.Running(s)
			if err != nil {
				return err
			}
			if running {
				return errors.New("stop the core before changing installation mode or ports")
			}
			if err := configurePorts(c, *mode, *mixed, *api, state.CheckPort); err != nil {
				return err
			}
			c.InstallMode = *mode
			if *mode == "user" {
				c.Tun = false
			}
			return nil
		})
		if err != nil {
			return err
		}
		return shellenv.Refresh(s)
	case "status":
		c, err := s.Read()
		if err != nil {
			return err
		}
		b, _ := json.Marshal(struct {
			InstallMode, Mode, Core, Profile string
			MixedPort, APIPort               int
		}{c.InstallMode, c.Mode, c.Core, c.Current, c.MixedPort, c.APIPort})
		fmt.Println(string(b))
		return nil
	}
	return errors.New("unknown command; use --help")
}

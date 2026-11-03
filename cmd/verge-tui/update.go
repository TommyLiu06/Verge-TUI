package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"verge-tui/internal/app"
	"verge-tui/internal/installation"
	"verge-tui/internal/state"
	"verge-tui/internal/tui"
	"verge-tui/internal/updater"
)

func runUpdateCommand(root string, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: update status|check|download|apply|configure")
	}
	flags := flag.NewFlagSet("update", flag.ContinueOnError)
	expected := flags.String("version", "", "verified update version to apply")
	automatic := flags.Bool("automatic", false, "recheck the saved idle update policy before applying")
	check := flags.Int("check-minutes", -1, "automatic check interval in minutes (0 = Manual)")
	autoDownload := flags.Bool("auto-download", false, "download detected releases automatically; requires automatic checking")
	idle := flags.Int("idle-minutes", -1, "continuous idle minutes before automatic update (0 = Manual)")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected update argument")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	b, err := sharedBackend(abs)
	if err != nil {
		return err
	}
	if b == nil {
		b = app.New(&state.Store{Root: abs}, version)
	}
	defer b.Close(context.Background())
	u := updater.Updater{Store: b.Store, Version: version}
	switch args[0] {
	case "status", "check", "download":
		page, err := b.ReadPage(ctx, "app-update", args[0])
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(page.Update)
	case "configure":
		fields := map[string]string{}
		if *check != -1 {
			fields["check-minutes"] = strconv.Itoa(*check)
		}
		if *idle != -1 {
			fields["idle-minutes"] = strconv.Itoa(*idle)
		}
		flags.Visit(func(f *flag.Flag) {
			if f.Name == "auto-download" {
				fields["auto-download"] = strconv.FormatBool(*autoDownload)
			}
		})
		if len(fields) == 0 {
			return errors.New("specify --check-minutes, --auto-download or --idle-minutes")
		}
		return b.Execute(ctx, app.Action{Kind: "update-options", Fields: fields})
	case "apply":
		if b.Remote != nil {
			return errors.New("system installation updates require root; open Check for Updates in the TUI")
		}
		if *expected == "" {
			return errors.New("specify the verified release with --version")
		}
		return u.Apply(ctx, *expected, *automatic, os.Stdout)
	}
	return errors.New("unknown update command")
}

func runTUI(ctx context.Context, b *app.Backend, args []string) (runErr error) {
	defer func() { runErr = errors.Join(runErr, b.Close(context.Background())) }()
	flags := flag.NewFlagSet("tui", flag.ContinueOnError)
	openUpdates := flags.Bool("open-updates", false, "open Check for Updates on startup")
	startCore := flags.Bool("start-core", false, "restore a foreground core after an application update")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected tui argument")
	}
	if *startCore {
		if b.Remote != nil {
			return errors.New("the system service owns this core")
		}
		if err := b.Manager.Start(ctx); err != nil {
			return err
		}
	}
	err := tui.Run(ctx, b, tui.RunOptions{OpenUpdates: *openUpdates})
	var request *updater.Request
	if errors.As(err, &request) {
		return finishUpdateRequest(ctx, b.Store.Root, request, "tui")
	}
	return err
}

// The TUI has restored the terminal and released any foreground core before
// entering sudo or the independent installer. No password is read by the UI.
func finishUpdateRequest(ctx context.Context, root string, request *updater.Request, resume string) error {
	prefix := filepath.Dir(root)
	summary, err := installation.Discover(prefix)
	if err != nil {
		return err
	}
	executable := filepath.Join(prefix, "verge-tui")
	args := []string{"--data-dir", root, "update"}
	if request.Configure != nil {
		args = append(args, "configure")
		for _, key := range []string{"check-minutes", "auto-download", "idle-minutes"} {
			if value, ok := request.ConfigureFields[key]; ok {
				args = append(args, "--"+key+"="+value)
			}
		}
	} else {
		args = append(args, "apply", "--version", request.Version)
		if request.Automatic {
			args = append(args, "--automatic")
		}
	}
	if !request.RestartOnly {
		command := exec.CommandContext(ctx, executable, args...)
		if summary.Mode == "system" && os.Geteuid() != 0 {
			fmt.Println("Administrator authorization is required to update this system installation.")
			command = exec.CommandContext(ctx, "sudo", append([]string{"--", executable}, args...)...)
		}
		command.Cancel = func() error { return command.Process.Signal(syscall.SIGTERM) }
		command.WaitDelay = 2 * time.Minute
		command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
		if err := command.Run(); err != nil {
			fmt.Fprintln(os.Stderr, "Update action failed:", err)
			if ctx.Err() != nil {
				return ctx.Err()
			}
			fmt.Println("Press Enter to return to Verge-TUI.")
			bufio.NewReader(os.Stdin).ReadString('\n')
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	resumeArgs := []string{executable, "--data-dir", root, resume}
	if resume == "tui" && request.Configure != nil {
		resumeArgs = append(resumeArgs, "--open-updates")
	}
	if resume == "tui" && request.ResumeCore {
		resumeArgs = append(resumeArgs, "--start-core")
	}
	return syscall.Exec(executable, resumeArgs, os.Environ())
}

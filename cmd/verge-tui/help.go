package main

import (
	"fmt"
	"io"
	"strings"
)

const rootHelp = `Verge TUI — a lightweight interface for an application-managed mihomo core.

Usage:
  verge-tui [--data-dir DIR] [command] [arguments]
  verge-tui help [command [subcommand]]
  verge-tui [command [subcommand]] --help

Global options (place before the command):
  --data-dir DIR    Application data directory (default: data beside the executable)
  -h, --help        Show help and exit successfully

Commands:
  tui              Open the keyboard/mouse interface (default)
  run              Keep the managed core running in the foreground; used by systemd
  core             Install, import, list or select managed mihomo versions
  profile          Import, create, list or select profiles
  update           Check, download, configure or apply application updates from GitHub
  env              Print shell exports for the proxy; does not change the parent shell
  env-file         Write proxy.env beside the data directory
  shell            Install/uninstall PATH configuration and verge-on/off/status functions
  proxy-status     Show core, proxy listener and current exported environment status
  service          Print a systemd unit; does not install or enable the service
  configure        Configure installation mode and ports while the core is stopped
  status           Print installation/core/profile/port settings as JSON
  preview          Print a plain-text page preview without opening the TUI
  version          Print the application version
  help             Show global or command help

Installation helpers:
  installation     Bundle verification, discovery, update and command-link management
  prepare-uninstall  Check core ownership and restore managed desktop proxy settings
  desktop-restore  Restore this user's managed desktop proxy settings

Examples:
  verge-tui
  verge-tui core import v1.19.32 ./mihomo
  verge-tui profile import 'https://example.com/subscription'
  verge-tui shell install
  verge-tui proxy-status
  verge-tui preview --page settings

Shell integration:
  shell install references bundled Bash/Zsh and fish scripts in the installation directory.
  Source the printed loader once for this shell; new shells load it automatically.
  verge-on loads proxy.env; verge-off clears proxy variables; verge-status checks status.
  Only state keywords are colored: enabled/true/healthy green; disabled/false/unavailable red.
  Redirected output, TERM=dumb and NO_COLOR disable colors.
  Bash/Zsh use a short rc loader; fish uses conf.d/ and the bundled autoload directory.
  shell uninstall removes user loaders; uninstall.sh also removes the bundled scripts.
  Only user mode adds the executable directory to PATH; system mode uses its command link.

System mode:
  Ordinary users manage shared profiles, nodes, TUN and cores through the running service.
  Only root manages the system service. TUN requires a system installation.
  External Controller is disabled by default; private Unix IPC keeps the TUI working.

Run 'verge-tui COMMAND --help' for command options and examples.
`

var helpTopics = map[string]string{
	"update":                     "Usage: verge-tui update status | check | download | apply --version VERSION | configure [options]\n\nCheck official TommyLiu06/Verge-TUI stable GitHub releases and verify offline bundles.\nThe running managed proxy is used for checks and downloads.\nUpdates preserve profiles, preferences, credentials and the selected mihomo core.\nUse Settings > Check for Updates for the complete download, authorization and restart flow.\nAutomatic policies run in the managed service, or while the TUI is open without a service.\nAutomatic checking and installation default to Manual; automatic download defaults to Off.\nEnable checking before automatic downloading, and downloading before automatic installation.\nSystem updates and policy changes require root.\n",
	"update status":              "Usage: verge-tui update status\n\nPrint cached release, download and automatic-update policy state as JSON.\nDoes not contact GitHub.\n",
	"update check":               "Usage: verge-tui update check\n\nCheck the latest stable official GitHub release and cache its metadata.\nThe published asset must include a SHA256 digest and matching checksum file.\n",
	"update download":            "Usage: verge-tui update download\n\nRecheck the latest release, delete a superseded cache, then download and verify the current release.\nDoes not stop the proxy or apply the update. Use the TUI to update and restart.\n",
	"update apply":               "Usage: verge-tui update apply --version VERSION [--automatic]\n\nApply a previously downloaded, verified version to this installation.\nA foreground-owned core must already be stopped. System installations require root.\nThe original service state is restored; installation failures roll back.\n--automatic also checks the saved policy and idle connections immediately before replacement.\n",
	"update configure":           "Usage: verge-tui update configure [--check-minutes N] [--auto-download[=true|false]] [--idle-minutes N]\n\n--check-minutes: 0 = Manual; 1-10080 = automatic check interval.\n--auto-download: immediately download detected updates; requires automatic checking.\n--idle-minutes: 0 = Manual; 1-1440 = continuous idle period before applying an update.\nAutomatic installation requires automatic downloads. Disabling a parent disables its children.\nThese installation-wide settings persist across sessions; system changes require root.\nTraffic, active connections or a failed connection probe restart the idle period.\n",
	"core available":             "Usage: verge-tui core available\n\nList compatible verified release assets from MetaCubeX/mihomo on GitHub.\nIncludes stable and compatible Alpha releases; follows official pagination.\nUses the running managed proxy automatically; otherwise honors proxy environment variables.\nThe TUI loads pages only when opening Install Core or choosing Load More/Refresh.\n",
	"shell info":                 "Usage: verge-tui shell info [--user UID]\n\nReport the existing registered configuration path and shell type.\nInspecting another account requires root. Does not initialize application data.\n",
	"installation account":       "Usage: verge-tui installation account --user UID\n\nRead the initiating account name and home for a sudo system installation.\nThis helper does not register functions or modify account files.\n",
	"installation retain":        "Usage: verge-tui installation retain --prefix DIR\n\nUninstaller helper: mark public discovery metadata as retained data.\nThe recorded command directory remains available for reinstallation.\n",
	"installation unlink":        "Usage: verge-tui installation unlink --prefix DIR\n\nUninstaller helper: remove only the recorded link still targeting this installation.\nA command replaced by another application is preserved.\n",
	"installation record":        "Usage: verge-tui installation record --prefix DIR [--bin-dir DIR]\n\nInstaller helper: record public installation metadata and create its system command link.\nSystem command registration requires root. Default directory: /usr/local/bin.\n",
	"installation check-link":    "Usage: verge-tui installation check-link --prefix DIR [--bin-dir DIR]\n\nCheck the command path without changing files. Default directory: /usr/local/bin.\nOnly an absent path or a link targeting this installation is accepted.\n",
	"installation discover":      "Usage: verge-tui installation discover --prefix DIR\n\nRead public installation mode/version/state and command-link metadata.\nNo configuration secrets are read. Menu hints still require private inspection after elevation.\n",
	"tui":                        "Usage: verge-tui [--data-dir DIR] tui\n\nOpen the TUI (also the default when no command is given).\nKeyboard: 1–7 pages, Tab/arrows focus, Enter activate, F1 help, q quit.\nMouse: click controls; wheel scrolls and clears focus.\nClosing a service-connected TUI leaves the service running.\n",
	"run":                        "Usage: verge-tui [--data-dir DIR] run\n\nStart the selected managed core and supervise it in the foreground.\nSIGINT/SIGTERM stops the owned core. System mode requires root.\nThe generated systemd unit runs this command.\n",
	"core":                       "Usage: verge-tui core available | install [TAG] | import ID FILE | list | use ID\n\n  available      List compatible releases from the official GitHub repository.\n  install [TAG]  Download an official mihomo release (default: latest).\n  import ID FILE Import an extracted mihomo executable into the managed core directory.\n  list           List installed versions; * marks the selection.\n  use ID         Select an installed version; restart the owned running core.\n\nInstalling/importing selects the new core only when no core is selected yet.\nExample: verge-tui core import vNEXT /path/to/mihomo\n",
	"core install":               "Usage: verge-tui core install [TAG]\n\nDownload and verify an official mihomo release into the managed directory.\nTAG defaults to latest; use core available to list official versions.\nA running managed proxy is used automatically, with private credentials in user mode.\nOtherwise HTTP(S) proxy environment variables apply. Network access is required.\nExample: verge-tui core install v1.19.32\n",
	"core import":                "Usage: verge-tui core import ID FILE\n\nImport and validate an extracted mihomo executable. No download is needed.\nID identifies the installed version; FILE is read by the calling user.\nExample: verge-tui core import vNEXT ./mihomo\n",
	"core list":                  "Usage: verge-tui core list\n\nList application-managed core versions; * marks the selected core.\n",
	"core use":                   "Usage: verge-tui core use ID\n\nSelect an installed core. The owning supervisor restarts a running core.\nIf another supervisor owns it, use that supervisor's TUI or stop its service first.\n",
	"profile":                    "Usage: verge-tui profile import URL | new NAME [YAML] | list | select ID\n\n  import URL      Download and add a subscription.\n  new NAME [YAML]  Add a local YAML file, or create a built-in DIRECT profile.\n  list            List profiles; * marks the active profile.\n  select ID       Select a profile and reload a running core.\n\nSystem-service clients also support: profile delete ID.\nDeletion is available in the TUI Profile Options menu in both installation modes.\nExamples:\n  verge-tui profile import 'https://example.com/subscription'\n  verge-tui profile new 'My Profile' ./config.yaml\n",
	"profile import":             "Usage: verge-tui profile import URL\n\nDownload a subscription, validate its YAML and store its usage/expiry metadata.\nImporting does not select the profile. Quote URLs that contain shell metacharacters.\n",
	"profile new":                "Usage: verge-tui profile new NAME [YAML]\n\nCreate a local profile from a YAML file. Omitting YAML creates a DIRECT profile.\nExample: verge-tui profile new 'My Profile' ./config.yaml\n",
	"profile list":               "Usage: verge-tui profile list\n\nList profile IDs, names and types; * marks the active profile.\n",
	"profile select":             "Usage: verge-tui profile select ID\n\nValidate and select the profile, reloading the running core if present.\n",
	"profile delete":             "Usage: verge-tui profile delete ID\n\nCLI deletion is available to system-service clients.\nUse Profile Options > Delete in the TUI for either installation mode.\nDeleting the active profile switches the core to the built-in DIRECT configuration.\n",
	"env":                        "Usage: verge-tui env [--shell bash|zsh|fish] [--auto-login]\n\nPrint proxy environment statements for the selected shell (default: Bash/POSIX).\nThis command cannot modify its parent shell. Prefer verge-on after shell install.\nUser-mode exports include proxy credentials.\n--auto-login prints nothing when this user has disabled Proxy on Login.\nFor fish: verge-tui env --shell fish | source\n",
	"env-file":                   "Usage: verge-tui env-file\n\nWrite proxy.env beside the application's data directory.\nUser mode uses private permissions and credentials; system mode uses shared loopback access.\n",
	"shell":                      "Usage: verge-tui shell install [--shell bash|zsh|fish] [--rc-file FILE]\n       verge-tui shell uninstall [--all]\n       verge-tui shell info\n\n  install         Register PATH and verge-on/off/status for this user.\n  --shell NAME   Override current-shell detection.\n  --rc-file FILE  Absolute startup-file path; default: existing registration or detected shell.\n  --user UID     Root installer only: register with the initiating account's UID/GID.\n  uninstall       Remove this user's registered loaders.\n  --all           Remove all registrations during administrator uninstallation.\n  info            Report the existing configuration path and shell.\n\nBash/Zsh source the installed shell/proxy.sh from a short rc loader.\nfish loads shell/fish/init.fish from conf.d/verge-tui.fish; config.fish is unchanged.\nFunction implementations ship in the package, not user configuration directories.\nOnly user mode adds the executable directory to PATH.\nInstallation requires the bundled shell scripts and proxy.env. Repeated registration is idempotent.\n",
	"shell install":              "Usage: verge-tui shell install [--shell bash|zsh|fish] [--rc-file FILE] [--user UID]\n\nRegister references to the installed verge-on/off/status scripts.\nNo function implementation files are generated. Only user mode changes PATH.\nAn existing registration is preserved; otherwise current-shell detection is used.\nBash: ~/.bashrc; Zsh: $ZDOTDIR/.zshrc or ~/.zshrc.\nfish: $XDG_CONFIG_HOME/fish or ~/.config/fish; config.fish is not modified.\nFILE selects an absolute startup-file path; use --shell for custom fish paths.\n--user is for the root installer to drop privileges before registering another account.\nSource the printed loader once or open a new shell.\n",
	"shell uninstall":            "Usage: verge-tui shell uninstall [--all]\n\nRemove the calling user's registered loader and user-mode PATH configuration.\nBundled scripts remain installed for other users; uninstall.sh removes them.\nUnrelated user files and startup settings are preserved.\n--all cleans every registered user during administrator uninstallation.\nAlready-running shells retain loaded functions until restarted.\n",
	"proxy-status":               "Usage: verge-tui proxy-status\n\nReport core/API availability, the proxy listener and exported proxy variables.\nVariables are classified as unset, matching, empty or different; passwords are hidden.\nUnexported parent-shell variables are not visible. No external website is contacted.\n",
	"service":                    "Usage: verge-tui service [--mode user|system]\n\nPrint a systemd unit for this executable and data directory. Default: user.\nThis command does not install or enable the unit; install.sh handles that.\nUser boot startup depends on user-service/lingering availability.\nSystem service administration requires root.\n",
	"configure":                  "Usage: verge-tui configure [--install-mode user|system] [--mixed-port PORT] [--api-port PORT]\n\n  --install-mode  Installation mode (default: user); system requires root.\n  --mixed-port    HTTP/SOCKS proxy port; 0 preserves/selects the default.\n  --api-port      Default external-controller port; 0 preserves/selects the default.\n\nStop the core before configuring mode or ports. Port range: 1024–65535.\nFresh system mode defaults: mixed 7890, external controller address 127.0.0.1:9090.\nThe external controller remains disabled until enabled in Settings.\nFresh user mode uses random high ports. Existing ports are preserved by default.\n",
	"status":                     "Usage: verge-tui status\n\nPrint configured installation mode, proxy mode, core, profile and ports as JSON.\nDoes not print secrets. Use proxy-status for live listener/environment checks.\n",
	"preview":                    "Usage: verge-tui preview [--page PAGE] [--width N] [--height N]\n\n  --page    home, proxies, profiles, connections, rules, logs or settings (default: home)\n  --width   Terminal width (default: 120)\n  --height  Terminal height (default: 42)\n\nDimensions must be between 1 and 500. Prints text without starting an interactive screen.\n",
	"version":                    "Usage: verge-tui version\n\nPrint the application version.\n",
	"prepare-uninstall":          "Usage: verge-tui prepare-uninstall\n\nInstaller helper: require a stopped core and restore managed desktop proxy settings.\nUse uninstall.sh for normal uninstallation and optional data removal.\n",
	"desktop-restore":            "Usage: verge-tui desktop-restore\n\nRestore this user's saved desktop proxy settings. Used by uninstall.sh.\n",
	"installation":               "Usage: verge-tui installation COMMAND [options]\n\nInstaller helpers:\n  discover --prefix DIR             Read public installation status\n  inspect --prefix DIR              Verify private installation state\n  update --prefix DIR [--rc-file FILE]\n  check-link --prefix DIR [--bin-dir DIR]\n  record --prefix DIR [--bin-dir DIR]\n  unlink --prefix DIR\n  retain --prefix DIR\n  account --user UID\n  check-ports [--mixed-port PORT] [--api-port PORT]\n  check-geodata --source-dir DIR --core-file FILE\n  seed-geodata --prefix DIR\n\nRun installation COMMAND --help for details.\n",
	"installation inspect":       "Usage: verge-tui installation inspect --prefix DIR\n\nRead an installation marker and verify the installed or retained data layout.\nDoes not initialize data or modify the destination. Output is intended for install.sh.\n",
	"installation update":        "Usage: verge-tui installation update --prefix DIR [--rc-file FILE]\n\nUpdate from the extracted offline bundle, preserving managed data and installation mode.\nUse install.sh for interactive preflight/confirmation and normal updates.\n",
	"installation check-ports":   "Usage: verge-tui installation check-ports [--mixed-port PORT] [--api-port PORT]\n\nCheck TCP/UDP availability of nonzero requested ports; zero skips that port.\nThe proxy and API ports must differ. Does not create an installation.\n",
	"installation check-geodata": "Usage: verge-tui installation check-geodata --source-dir DIR --core-file FILE\n\nVerify bundled geography assets and test them using the specified mihomo executable.\n",
	"installation seed-geodata":  "Usage: verge-tui installation seed-geodata --prefix DIR\n\nProvision bundled geography assets into an installation's runtime directory.\nDIR must be absolute. Valid existing assets are retained.\n",
	"shell remove":               "Usage: verge-tui shell remove [--all]\n\nAlias for shell uninstall. Removes the registered functions and PATH configuration from rc.\nSee verge-tui shell uninstall --help.\n",
}

// Help is handled before opening the store or contacting a system service.
func printHelp(args []string, out io.Writer) (bool, error) {
	var words []string
	requested := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--data-dir" {
			i++
			continue
		}
		if strings.HasPrefix(arg, "--data-dir=") {
			continue
		}
		if arg == "--help" || arg == "-h" || arg == "-help" {
			requested = true
			continue
		}
		if arg == "help" && len(words) == 0 {
			requested = true
			continue
		}
		if strings.HasPrefix(arg, "-") {
			switch arg {
			case "--bin-dir", "--user", "--shell", "--rc-file", "--page", "--width", "--height", "--mode", "--install-mode", "--mixed-port", "--api-port", "--prefix", "--source-dir", "--core-file", "--check-minutes", "--idle-minutes", "--version":
				i++
			}
			continue
		}
		words = append(words, arg)
	}
	if !requested {
		return false, nil
	}
	if len(words) == 0 {
		_, err := io.WriteString(out, rootHelp)
		return true, err
	}
	topic := words[0]
	if len(words) > 1 {
		if _, ok := helpTopics[strings.Join(words[:2], " ")]; ok {
			topic = strings.Join(words[:2], " ")
		}
	}
	text, ok := helpTopics[topic]
	if !ok {
		return true, fmt.Errorf("unknown help topic %q; use verge-tui --help", topic)
	}
	_, err := io.WriteString(out, text+"\nGlobal: --data-dir DIR (before command); -h, --help.\n")
	return true, err
}

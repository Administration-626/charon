// Command charon detects the Codex, Claude Code, OpenCode, Pi, and Grok CLIs and
// switches their endpoint + credentials between saved bindings.
package main

import (
	"fmt"
	"os"

	"charon/internal/catalog"
	"charon/internal/tui"
)

// version is set at build time via -ldflags (see .goreleaser.yaml).
var version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "charon: "+err.Error())
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "update":
			return cmdUpdate()
		case "uninstall":
			return cmdUninstall()
		case "version", "-v", "--version":
			fmt.Println("charon " + version)
			return nil
		case "help", "-h", "--help":
			printUsage()
			return nil
		}
	}

	cat, err := catalog.Open()
	if err != nil {
		return err
	}

	if len(args) == 0 {
		return tui.Run(cat, version)
	}

	switch args[0] {
	case "status", "st":
		return cmdStatus(cat, args[1:])
	case "ls":
		return cmdList(cat, args[1:])
	case "switch", "use":
		return cmdSwitch(cat, args[1:])
	case "models":
		return cmdModels(args[1:])
	case "add":
		return cmdAdd(cat, args[1:])
	case "edit":
		return cmdEdit(cat, args[1:])
	case "rename", "mv":
		return cmdRename(cat, args[1:])
	case "cp":
		return cmdDuplicate(cat, args[1:])
	case "rm":
		return cmdRemove(cat, args[1:])
	case "completion":
		return cmdCompletion(args[1:])
	case "__profiles": // hidden: feeds shell completion
		return cmdProfiles(cat, args[1:])
	default:
		printUsage()
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func printUsage() {
	fmt.Print(`charon — detect and switch AI tool endpoints + credentials

Usage:
  charon                     interactive menu
  charon status              show each tool's live config and active binding (--json)
  charon ls <tool>           list saved bindings for a tool (--json)
  charon models <tool>       list models from an API (--key, --endpoint)
  charon add <tool>          add+activate a binding (--name --key plus a model id)
  charon edit <tool> <b>     change a binding's endpoint/key/model/models/name
  charon rename <tool> <o> <n>  rename a saved binding
  charon cp <tool> <src> <dst>  duplicate a saved binding
  charon cp <tool> <src> <tool> <dst>  copy a binding to another tool
  charon switch <tool> <b>   render a saved binding into the tool
  charon rm <tool> <b>       delete a saved binding (not the active one)
  charon completion <shell>  print a bash/zsh/fish completion script
  charon update              upgrade charon to the latest version
  charon uninstall           remove the installed charon binary

Tools: codex, claude, opencode, pi, grok
`)
}

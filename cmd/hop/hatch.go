package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"grasshopper/internal/registry"
	"grasshopper/internal/sessions"
)

// serverName is what agents call grasshopper in their own configuration.
const serverName = "grasshopper"

// runHatch wires grasshopper into every agent on this machine and shows somebody
// around.
//
// It is the first command anybody runs, which makes it the only one worth spending
// a little personality on: this is where a person decides whether they are holding
// a piece of software somebody cared about or a background task that turned up
// uninvited. Everything after this is terse on purpose.
func runHatch(args []string) error {
	fs := flags("hatch", "")
	if _, err := parse(fs, args); err != nil {
		return err
	}

	fmt.Printf("\n  🦗  %s %s\n\n", accent("grasshopper"), dim(version))

	binary, err := os.Executable()
	if err != nil {
		binary = "hop"
	}

	fmt.Printf("  %s\n\n", dim("Wiring it into your agents…"))
	registered := 0
	for _, target := range mcpTargets() {
		if err := target.register(binary); err != nil {
			fmt.Printf("     %s %s\n", pad(target.name, 26), dim("could not register: "+err.Error()))
			continue
		}
		fmt.Printf("     %s %s\n", pad(target.name, 26), good("✓"))
		registered++
	}
	if registered == 0 {
		fmt.Printf("     %s\n", dim("nothing on this machine speaks MCP yet"))
	}

	all, err := sessions.List()
	if err != nil {
		return err
	}
	naming := namer()
	counts := map[string]int{}
	for _, s := range all {
		counts[naming(s)]++
	}

	fmt.Printf("\n  %s\n\n", dim("Looking around…"))
	if len(all) == 0 {
		fmt.Printf("     %s\n", dim("no sessions yet — they turn up here the moment you have one"))
		fmt.Printf("     %s\n", dim("hop source shows where grasshopper is looking"))
	} else {
		fmt.Printf("     %s in %s\n", bold(plural2(len(all), "session", "sessions")), plural(len(counts)))
		for i, name := range sortedByCount(counts) {
			if i == 3 {
				fmt.Printf("        %s\n", dim(fmt.Sprintf("…and %d more", len(counts)-3)))
				break
			}
			fmt.Printf("        %s %s\n", pad(name, 26), dim(fmt.Sprintf("%d", counts[name])))
		}
	}

	fmt.Printf("\n  %s\n\n", dim("Two ways to use it"))
	fmt.Print("     Ask any agent, in any session:\n")
	fmt.Printf("        %s\n\n", accent(`"bring me the thread where I worked on billing"`))
	fmt.Print("     Or from here:\n")
	for _, line := range [][2]string{
		{"hop pack", "pick a session — it lands on your clipboard"},
		{"hop to", "open one in another agent"},
		{"hop ls", "everything grasshopper can read"},
	} {
		// Padded before it is coloured: an escape sequence is bytes a width
		// specifier counts and a terminal does not.
		fmt.Printf("        %s %s\n", accent(pad(line[0], 10)), dim(line[1]))
	}

	fmt.Printf("\n  Your hops live in %s. %s\n\n",
		registry.Home(), dim("Nothing ever leaves this machine."))
	return nil
}

// runUninstall takes grasshopper back out of every agent it registered with.
//
// Unregistering before the binary goes is the whole reason it exists: an agent left
// pointing at a command that is no longer there fails on every start, and it reads
// as the agent misbehaving rather than as grasshopper.
func runUninstall(args []string) error {
	fs := flags("uninstall", "")
	if _, err := parse(fs, args); err != nil {
		return err
	}

	removed, stuck := 0, 0
	for _, target := range mcpTargets() {
		// Saying "unregistered" about an agent that offers no way to unregister
		// would be the one lie a cleanup command must not tell.
		if !target.removable() {
			fmt.Printf("  %-26s remove it in the app: it takes a server but gives none back\n", target.name)
			stuck++
			continue
		}
		if err := target.unregister(); err != nil {
			fmt.Printf("  %-26s could not unregister: %v\n", target.name, err)
			continue
		}
		fmt.Printf("  %-26s unregistered\n", target.name)
		removed++
	}
	if removed == 0 && stuck == 0 {
		fmt.Print("  nothing was registered\n")
	}

	if binary, err := os.Executable(); err == nil {
		fmt.Printf("\nThe binary is still at %s — delete it when you are ready.\n", binary)
	}
	fmt.Printf("Your hops are in %s and were not touched.\n", registry.Home())
	return nil
}

// mcpTargets is every agent that can be told about an MCP server, with the
// arguments its own command line wants. The shapes differ per agent for no
// predictable reason — one takes a --scope, the next has never heard of it — so
// they live in the registry as data, and a third agent costs a line of JSON.
func mcpTargets() []mcpTarget {
	reg, err := registry.Load()
	if err != nil {
		return nil
	}
	var targets []mcpTarget
	for _, key := range reg.Keys() {
		agent := reg[key]
		// An agent with a config file and no command line is still reachable — it
		// is just reached by writing rather than by asking. When it has both, the
		// command line wins: the agent owns its own format.
		if config := registry.ConfigPath(agent); len(agent.MCPAdd) == 0 && config != "" && agent.MCPConfigKey != "" {
			targets = append(targets, mcpTarget{
				name:      reg.Called(key),
				config:    config,
				configKey: agent.MCPConfigKey,
			})
			continue
		}
		if len(agent.MCPAdd) == 0 {
			continue
		}
		command, ok := registry.Launcher(agent)
		if !ok {
			continue
		}
		targets = append(targets, mcpTarget{
			name:    reg.Called(key),
			command: command,
			add:     agent.MCPAdd,
			remove:  agent.MCPRemove,
		})
	}
	return targets
}

type mcpTarget struct {
	name    string
	command string
	add     []string
	remove  []string

	// config and configKey are set instead of command for an agent that has no
	// command line to ask.
	config    string
	configKey string
}

// removable reports whether this agent can be told to forget as well as told to
// remember. Some can only be told once — the VS Code family takes a server on
// the command line and offers no way to take one back — and hop uninstall says
// so rather than reporting a success it did not have.
func (t mcpTarget) removable() bool {
	return t.config != "" || len(t.remove) > 0
}

// register points an agent at grasshopper. The removal comes first, so running
// hatch again is a repair rather than a second copy.
func (t mcpTarget) register(binary string) error {
	if t.config != "" {
		return writeMCPConfig(t.config, t.configKey, serverName, binary)
	}
	if len(t.remove) > 0 {
		_ = exec.Command(t.command, registry.MCPArgs(t.remove, serverName, binary)...).Run()
	}
	out, err := exec.Command(t.command, registry.MCPArgs(t.add, serverName, binary)...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s", lastLine(string(out)))
	}
	return nil
}

func (t mcpTarget) unregister() error {
	if t.config != "" {
		return removeMCPConfig(t.config, t.configKey, serverName)
	}
	if len(t.remove) == 0 {
		return nil
	}
	out, err := exec.Command(t.command, registry.MCPArgs(t.remove, serverName, "")...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s", lastLine(string(out)))
	}
	return nil
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) == 0 {
		return s
	}
	return strings.TrimSpace(lines[len(lines)-1])
}

// pad widens plain text to a column, for a caller that is about to colour it.
func pad(s string, width int) string {
	if n := len([]rune(s)); n < width {
		return s + strings.Repeat(" ", width-n)
	}
	return s
}

// One accent, one glyph, each used once. Anything more and a command line starts
// feeling like a billboard. The colours are from the sixteen every terminal theme
// defines, so they read on a light ground as well as a dark one.
func accent(s string) string { return "\x1b[36m" + s + "\x1b[0m" }
func bold(s string) string   { return "\x1b[1m" + s + "\x1b[0m" }
func dim(s string) string    { return "\x1b[2m" + s + "\x1b[0m" }
func good(s string) string   { return "\x1b[32m" + s + "\x1b[0m" }

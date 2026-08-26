package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"grasshopper/internal/registry"
	"grasshopper/internal/sessions"
)

// runSetup wires grasshopper into every agent on this machine that can reach it,
// and says what it found.
//
// It exists so that installing is one step instead of two. A binary on PATH that
// no agent knows about carries nothing, and the gap between those two states is
// where somebody decides the tool does not work.
// serverName is what agents call grasshopper in their own configuration.
const serverName = "grasshopper"

// runUninstall takes grasshopper back out of every agent it registered with.
//
// Unregistering before the binary goes is the whole point of it existing: an agent
// left pointing at a command that is no longer there fails on every start, and it
// reads as the agent misbehaving rather than as grasshopper.
func runUninstall(args []string) error {
	fs := flags("uninstall", "")
	if _, err := parse(fs, args); err != nil {
		return err
	}

	removed := 0
	for _, target := range mcpTargets() {
		if err := target.unregister(); err != nil {
			fmt.Printf("  %-14s could not unregister: %v\n", target.name, err)
			continue
		}
		fmt.Printf("  %-14s unregistered\n", target.name)
		removed++
	}
	if removed == 0 {
		fmt.Print("  nothing was registered\n")
	}

	binary, err := os.Executable()
	if err == nil {
		fmt.Printf("\nThe binary is still at %s — delete it when you are ready.\n", binary)
	}
	fmt.Printf("Your hops are in %s and were not touched.\n", registry.Home())
	return nil
}

func runSetup(args []string) error {
	fs := flags("setup", "")
	if _, err := parse(fs, args); err != nil {
		return err
	}

	binary, err := os.Executable()
	if err != nil {
		binary = "hop"
	}

	// Registration goes through each agent's own command line, so grasshopper
	// never edits somebody else's configuration file by hand.
	registered, failed := 0, 0
	for _, target := range mcpTargets() {
		if err := target.register(binary); err != nil {
			fmt.Printf("  %-14s could not register: %v\n", target.name, err)
			failed++
			continue
		}
		fmt.Printf("  %-14s registered\n", target.name)
		registered++
	}

	if registered == 0 && failed == 0 {
		fmt.Print("  no agent on this machine speaks MCP yet — nothing to register\n")
	}

	all, err := sessions.List()
	if err != nil {
		return err
	}
	fmt.Printf("\n%d sessions readable. Ask any agent to bring you one, or run hop ls.\n", len(all))
	return nil
}

// mcpTargets is every agent that can be told about an MCP server, with the
// arguments its own command line wants. The shapes differ per agent for no
// predictable reason, so they live in the registry as data.
func mcpTargets() []mcpTarget {
	reg, err := registry.Load()
	if err != nil {
		return nil
	}
	var targets []mcpTarget
	for _, key := range reg.Keys() {
		agent := reg[key]
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
}

// register points an agent at grasshopper. The removal comes first so re-running
// setup is a repair rather than a second copy.
func (t mcpTarget) register(binary string) error {
	if len(t.remove) > 0 {
		_ = exec.Command(t.command, registry.MCPArgs(t.remove, serverName, binary)...).Run()
	}
	out, err := exec.Command(t.command, registry.MCPArgs(t.add, serverName, binary)...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s", lastLine(string(out)))
	}
	return nil
}

// unregister takes it back out, for an uninstall that leaves nothing calling a
// command that is no longer there.
func (t mcpTarget) unregister() error {
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

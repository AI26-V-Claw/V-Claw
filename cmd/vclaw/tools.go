package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"vclaw/internal/tools"
	sandboxtool "vclaw/internal/tools/system/sandbox"
	webtool "vclaw/internal/tools/web"
)

func runTools(_ context.Context, args []string) error {
	if len(args) == 0 {
		printToolsUsage()
		return nil
	}

	switch args[0] {
	case "list":
		return runToolsList(args[1:])
	case "help", "-h", "--help":
		printToolsUsage()
		return nil
	default:
		return fmt.Errorf("unknown tools command %q", args[0])
	}
}

func runToolsList(args []string) error {
	fs := flag.NewFlagSet("vclaw tools list", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	groupFilter := fs.String("group", "", "filter by tool group")
	if err := fs.Parse(args); err != nil {
		return err
	}

	registry := tools.NewToolRegistry()
	_ = tools.RegisterBuiltInTools(registry)
	_ = sandboxtool.RegisterToolsWithConfig(registry, sandboxtool.Config{})
	_ = webtool.RegisterTools(registry, webtool.NewService(nil))
	// Note: Google Workspace tools require OAuth and are not listed here.
	// Run with a live agent to see all registered tools.

	var defs []tools.ToolDefinition
	filter := strings.TrimSpace(*groupFilter)
	if filter != "" {
		defs = registry.ListToolsByGroup(filter)
	} else {
		defs = registry.ListTools()
	}

	if len(defs) == 0 {
		if filter != "" {
			fmt.Printf("No tools found in group %q.\n", filter)
		} else {
			fmt.Println("No tools registered.")
		}
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tGROUP\tCAPABILITY\tRISK\tAPPROVAL\tENABLED")
	fmt.Fprintln(w, "----\t-----\t----------\t----\t--------\t-------")
	for _, d := range defs {
		approval := "no"
		if d.RequiresApproval {
			approval = "yes"
		}
		enabled := "yes"
		if !d.Enabled {
			enabled = "no"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			d.Name, d.Group, d.Capability, d.RiskLevel, approval, enabled)
	}
	w.Flush()

	enabledCount := 0
	for _, d := range defs {
		if d.Enabled {
			enabledCount++
		}
	}
	fmt.Printf("\nTotal: %d tools (%d enabled, %d disabled)\n",
		len(defs), enabledCount, len(defs)-enabledCount)
	fmt.Println("Note: Google Workspace tools are not shown. They require OAuth and are registered at runtime.")

	return nil
}

func printToolsUsage() {
	fmt.Println(`Usage:
  vclaw tools list [-group <group>]
      List all registered tools with metadata.

  vclaw tools list -group sandbox
      List only tools in the sandbox group.

Available groups: builtin, web, sandbox, google_workspace`)
}

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/oaooao/capx/internal/config"
	capxserver "github.com/oaooao/capx/internal/server"
	"github.com/oaooao/capx/internal/setup"
)

const version = "1.0.0"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	configPath := resolveConfigPath()

	switch os.Args[1] {
	case "serve":
		cmdServe(configPath)
	case "list":
		cmdList(configPath)
	case "scenes":
		cmdScenes(configPath)
	case "scene":
		cmdScene(configPath)
	case "add":
		cmdAdd(configPath)
	case "setup":
		cmdSetup(configPath)
	case "init":
		cmdInit()
	case "version":
		fmt.Printf("capx %s\n", version)
	case "help", "--help", "-h":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func resolveConfigPath() string {
	// --config flag
	for i, arg := range os.Args {
		if arg == "--config" && i+1 < len(os.Args) {
			return os.Args[i+1]
		}
		if strings.HasPrefix(arg, "--config=") {
			return strings.TrimPrefix(arg, "--config=")
		}
	}
	// CAPX_CONFIG env
	if p := os.Getenv("CAPX_CONFIG"); p != "" {
		return p
	}
	return config.DefaultConfigPath()
}

func cmdServe(configPath string) {
	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	scene := os.Getenv("CAPX_SCENE")
	if scene == "" {
		scene = cfg.DefaultScene
	}

	if err := capxserver.Serve(cfg, scene); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func cmdList(configPath string) {
	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	visible := cfg.VisibleCapabilities()
	for name, cap := range visible {
		tags := ""
		if len(cap.Tags) > 0 {
			tags = " [" + strings.Join(cap.Tags, ", ") + "]"
		}
		fmt.Printf("  %-30s %s — %s%s\n", name, cap.Type, cap.Description, tags)
	}
}

func cmdScenes(configPath string) {
	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	for name := range cfg.Scenes {
		fmt.Println(name)
	}
}

func cmdScene(configPath string) {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: capx scene list")
		os.Exit(1)
	}
	if os.Args[2] != "list" {
		fmt.Fprintf(os.Stderr, "unknown scene subcommand: %s\n", os.Args[2])
		os.Exit(1)
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	for name, scene := range cfg.Scenes {
		marker := " "
		if name == cfg.DefaultScene {
			marker = "*"
		}
		fmt.Printf(" %s %-20s [%s]\n", marker, name, strings.Join(scene.AutoEnable.All(), ", "))
	}
}

func cmdAdd(configPath string) {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: capx add <name> --type mcp|cli [--transport stdio|http] [--command cmd] [--url url] [--description desc]")
		os.Exit(1)
	}

	name := os.Args[2]
	cap := &config.Capability{}

	args := os.Args[3:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--type":
			i++
			cap.Type = args[i]
		case "--transport":
			i++
			cap.Transport = args[i]
		case "--command":
			i++
			cap.Command = args[i]
		case "--url":
			i++
			cap.URL = args[i]
		case "--description":
			i++
			cap.Description = args[i]
		case "--args":
			i++
			// Parse JSON array or comma-separated.
			val := args[i]
			if strings.HasPrefix(val, "[") {
				var parsed []string
				json.Unmarshal([]byte(val), &parsed)
				cap.Args = parsed
			} else {
				cap.Args = strings.Split(val, ",")
			}
		}
	}

	if cap.Type == "" {
		fmt.Fprintln(os.Stderr, "error: --type is required")
		os.Exit(1)
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		cfg = &config.Config{
			Capabilities: make(map[string]*config.Capability),
			Scenes:       map[string]*config.Scene{"default": {AutoEnable: config.AutoEnable{}}},
			DefaultScene: "default",
		}
	}

	cfg.Capabilities[name] = cap
	if err := config.Save(cfg, configPath); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Added capability %q to %s\n", name, configPath)
}

func cmdSetup(configPath string) {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: capx setup <claude-code|codex>")
		os.Exit(1)
	}

	var err error
	switch os.Args[2] {
	case "claude-code":
		err = setup.SetupClaudeCode(configPath)
	case "codex":
		err = setup.SetupCodex(configPath)
	default:
		fmt.Fprintf(os.Stderr, "unknown setup target: %s\n", os.Args[2])
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func cmdInit() {
	opts := setup.InitOptions{}
	for i := 2; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "--global":
			opts.Global = true
		case "--add-scenes":
			opts.AddScenes = true
		case "--force":
			opts.Force = true
		case "--agent":
			if i+1 >= len(os.Args) {
				fmt.Fprintln(os.Stderr, "--agent requires a value (claude-code | codex)")
				os.Exit(1)
			}
			opts.Agent = os.Args[i+1]
			i++
		default:
			if strings.HasPrefix(os.Args[i], "--agent=") {
				opts.Agent = strings.TrimPrefix(os.Args[i], "--agent=")
			} else {
				fmt.Fprintf(os.Stderr, "unknown flag: %s\n", os.Args[i])
				os.Exit(1)
			}
		}
	}
	if err := setup.Init(opts); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`capx — Agent Capability Runtime

Usage:
  capx serve                  Start MCP server (stdio)
  capx list                   List all configured capabilities
  capx scene list             List all scenes
  capx add <name> [options]   Add a capability to config
  capx setup claude-code      Migrate Claude Code to use capx
  capx setup codex            Migrate Codex CLI to use capx
  capx version                Print version

Flags:
  --config <path>             Config file path (default: ~/.config/capx/config.yaml)

Environment:
  CAPX_CONFIG                 Config file path override
  CAPX_SCENE                  Scene to use at startup`)
}

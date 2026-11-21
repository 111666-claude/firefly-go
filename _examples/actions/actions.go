package main

import "github.com/firefly-zero/firefly-go/firefly"

func init() {
	firefly.Boot = boot
	firefly.Update = update
	firefly.Render = render
	firefly.BeforeExit = beforeExit
}

// The file where the player's bindings are persisted.
const configFile = "actions.conf"

var (
	font firefly.Font
	game firefly.ActionMap

	// If true, the next button press is used to rebind "jump".
	rebinding bool
)

func boot() {
	font = firefly.LoadFile("font", nil).Font()

	// Define the actions and their default bindings.
	// jump uses the south button by default.
	if err := game.Define("jump", firefly.KeyS); err != nil {
		panic(err)
	}
	if err := game.Define("shoot", firefly.KeyN); err != nil {
		panic(err)
	}
	if err := game.Define("rebind", firefly.KeyW); err != nil {
		panic(err)
	}

	// Restore the bindings saved during a previous run.
	// If the file is missing or broken, the defaults are kept.
	if firefly.FileExists(configFile) {
		_ = game.UnmarshalText(firefly.LoadFile(configFile, nil).Bytes())
	}
}

func update() {
	// Read the normal input API and feed it into the action map.
	input := firefly.ReadRawInput(firefly.Combined)
	game.Feed(input)

	if game.Pressed("rebind") {
		rebinding = true
	}

	// A tiny settings screen: press S or E to make it the new jump key.
	if rebinding {
		switch {
		case input.Buttons.S:
			_ = game.Bind("jump", firefly.KeyS)
			rebinding = false
		case input.Buttons.E:
			_ = game.Bind("jump", firefly.KeyE)
			rebinding = false
		}
	}
}

func render() {
	firefly.ClearScreen(firefly.ColorWhite)
	color := firefly.ColorBlack
	if game.Held("jump") {
		color = firefly.ColorRed
	}
	firefly.DrawText("jump = "+keysString("jump"), font, firefly.P(10, 30), color)
	firefly.DrawText("shoot = "+keysString("shoot"), font, firefly.P(10, 50), color)
	hint := "press W, then S or E"
	if rebinding {
		hint = "press S or E to rebind jump"
	}
	firefly.DrawText(hint, font, firefly.P(10, 70), firefly.ColorDarkBlue)
}

func keysString(action string) string {
	keys := game.Keys(action)
	if len(keys) == 0 {
		return "unbound"
	}
	out := keys[0].String()
	for _, key := range keys[1:] {
		out += ", " + key.String()
	}
	return out
}

func beforeExit() {
	// Persist the current bindings so that they survive a restart.
	firefly.DumpFile(configFile, []byte(game.String()))
}

package firefly_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/firefly-zero/firefly-go/firefly"
)

func newMap(t *testing.T) *firefly.ActionMap {
	t.Helper()
	m := firefly.NewActionMap()
	for _, action := range []string{"jump", "shoot", "confirm"} {
		if err := m.Define(action); err != nil {
			t.Fatalf("define %q: %v", action, err)
		}
	}
	return m
}

func bind(t *testing.T, m *firefly.ActionMap, action string, keys ...firefly.Key) {
	t.Helper()
	if err := m.Bind(action, keys...); err != nil {
		t.Fatalf("bind %q: %v", action, err)
	}
}

func handle(t *testing.T, m *firefly.ActionMap, events ...firefly.Event) {
	t.Helper()
	if err := m.Handle(events...); err != nil {
		t.Fatalf("handle: %v", err)
	}
}

func press(key firefly.Key) firefly.Event {
	return firefly.Event{Key: key, Down: true}
}

func release(key firefly.Key) firefly.Event {
	return firefly.Event{Key: key, Down: false}
}

func TestActionMap_ZeroValue(t *testing.T) {
	t.Parallel()
	var m firefly.ActionMap
	handle(t, &m)
	bind(t, &m, "jump", firefly.KeyS)
	handle(t, &m, press(firefly.KeyS))
	if !m.Pressed("jump") || !m.Held("jump") || m.Released("jump") {
		t.Fatalf("zero-value map did not work: %+v", m)
	}
}

func TestActionMap_MultipleBindings(t *testing.T) {
	t.Parallel()
	m := newMap(t)
	bind(t, m, "jump", firefly.KeyE, firefly.KeyDPadUp)

	handle(t, m, press(firefly.KeyE))
	if !m.Pressed("jump") || !m.Held("jump") {
		t.Fatal("jump should be pressed and held after E")
	}

	handle(t, m, press(firefly.KeyDPadUp))
	if m.Pressed("jump") {
		t.Fatal("second bound key must not emit another press edge")
	}
	if !m.Held("jump") {
		t.Fatal("jump should stay held while E is down")
	}

	handle(t, m, release(firefly.KeyE))
	if m.Released("jump") || !m.Held("jump") {
		t.Fatal("jump should stay held while DPadUp is down")
	}

	handle(t, m, release(firefly.KeyDPadUp))
	if !m.Released("jump") || m.Held("jump") {
		t.Fatal("jump should be released after the last key goes up")
	}
}

func TestActionMap_RuntimeRebind(t *testing.T) {
	t.Parallel()
	m := newMap(t)
	bind(t, m, "jump", firefly.KeyS)
	handle(t, m, press(firefly.KeyS))
	if !m.Held("jump") {
		t.Fatal("jump should be held")
	}

	// Rebind to a left-handed key while the old key is still down.
	bind(t, m, "jump", firefly.KeyE)
	if m.Held("jump") {
		t.Fatal("jump must not stay held through a rebind")
	}
	if m.Released("jump") {
		t.Fatal("rebinding must not emit a release edge")
	}

	// The old key is now inert.
	handle(t, m, release(firefly.KeyS), press(firefly.KeyS))
	if m.Pressed("jump") || m.Held("jump") {
		t.Fatal("unbound key must not drive the action")
	}

	// The new key works immediately.
	handle(t, m, release(firefly.KeyS), press(firefly.KeyE))
	if !m.Pressed("jump") || !m.Held("jump") {
		t.Fatal("newly bound key should drive the action")
	}
}

func TestActionMap_RebindKeepsHeldWhenKeyStillBound(t *testing.T) {
	t.Parallel()
	m := newMap(t)
	bind(t, m, "jump", firefly.KeyS)
	handle(t, m, press(firefly.KeyS))
	if !m.Pressed("jump") {
		t.Fatal("jump should be pressed")
	}

	// The key remains bound, so the held state is unchanged and no edge
	// is emitted in either direction.
	handle(t, m)
	bind(t, m, "jump", firefly.KeyS, firefly.KeyE)
	if !m.Held("jump") {
		t.Fatal("jump should remain held")
	}
	if m.Pressed("jump") || m.Released("jump") {
		t.Fatal("rebind must not produce edges")
	}
}

func TestActionMap_SharedKey(t *testing.T) {
	t.Parallel()
	m := newMap(t)
	bind(t, m, "jump", firefly.KeyS)
	bind(t, m, "confirm", firefly.KeyS)

	handle(t, m, press(firefly.KeyS))
	if !m.Pressed("jump") || !m.Pressed("confirm") {
		t.Fatal("a shared key must press every action bound to it")
	}
	if !m.Held("jump") || !m.Held("confirm") {
		t.Fatal("a shared key must hold every action bound to it")
	}

	handle(t, m, release(firefly.KeyS))
	if !m.Released("jump") || !m.Released("confirm") {
		t.Fatal("a shared key must release every action bound to it")
	}

	// Overlapping actions don't interfere with each other.
	bind(t, m, "shoot", firefly.KeyS, firefly.KeyN)
	handle(t, m, press(firefly.KeyS))
	if !m.Held("jump") || !m.Held("confirm") || !m.Held("shoot") {
		t.Fatal("all actions sharing the key should be held")
	}
	handle(t, m, release(firefly.KeyS))
	if m.Held("shoot") {
		t.Fatal("shoot still has N bound and should be released")
	}
	if !m.Released("shoot") || !m.Released("jump") {
		t.Fatal("every affected action should get a release edge")
	}
}

func TestActionMap_DuplicateBindings(t *testing.T) {
	t.Parallel()
	m := newMap(t)
	bind(t, m, "jump", firefly.KeyS, firefly.KeyS, firefly.KeyE, firefly.KeyS)

	keys := m.Keys("jump")
	if len(keys) != 2 || keys[0] != firefly.KeyS || keys[1] != firefly.KeyE {
		t.Fatalf("duplicates must be dropped, got %v", keys)
	}

	handle(t, m, press(firefly.KeyS))
	if !m.Pressed("jump") || !m.Held("jump") {
		t.Fatal("jump should be held")
	}
	handle(t, m, release(firefly.KeyS))
	if !m.Released("jump") || m.Held("jump") {
		t.Fatal("duplicate binding must not keep the action held")
	}
}

func TestActionMap_DuplicateEvents(t *testing.T) {
	t.Parallel()
	m := newMap(t)
	bind(t, m, "jump", firefly.KeyS)

	handle(t, m, press(firefly.KeyS), press(firefly.KeyS))
	if !m.Pressed("jump") || !m.Held("jump") {
		t.Fatal("repeated press events are idempotent")
	}

	handle(t, m, release(firefly.KeyS))
	if !m.Released("jump") || m.Held("jump") {
		t.Fatal("jump should be released")
	}

	// Releasing a key that is not down must not emit an edge.
	handle(t, m, release(firefly.KeyS))
	if m.Released("jump") || m.Pressed("jump") || m.Held("jump") {
		t.Fatal("releasing an already released key must be a no-op")
	}
}

func TestActionMap_FocusLoss(t *testing.T) {
	t.Parallel()
	m := newMap(t)
	bind(t, m, "jump", firefly.KeyS)
	bind(t, m, "shoot", firefly.KeyN, firefly.KeyDPadLeft)
	handle(t, m, press(firefly.KeyS), press(firefly.KeyN), press(firefly.KeyDPadLeft))
	if !m.Held("jump") || !m.Held("shoot") {
		t.Fatal("actions should be held before focus loss")
	}

	m.ReleaseAll()
	if !m.Released("jump") || !m.Released("shoot") {
		t.Fatal("focus loss must release all actions")
	}
	if m.Held("jump") || m.Held("shoot") || m.Pressed("jump") {
		t.Fatal("nothing should be held after focus loss")
	}

	// Calling it again does not repeat edges.
	m.ReleaseAll()
	if m.Released("jump") || m.Released("shoot") {
		t.Fatal("second ReleaseAll must not emit release edges")
	}

	// Pressing again after focus loss works from a clean state.
	handle(t, m, press(firefly.KeyS))
	if !m.Pressed("jump") || !m.Held("jump") {
		t.Fatal("action should be pressable after focus loss")
	}
}

func TestActionMap_VeryFastTaps(t *testing.T) {
	t.Parallel()
	m := newMap(t)
	bind(t, m, "jump", firefly.KeyS)

	// Press and release within one frame: both edges are visible and the
	// action is not held at the end of the frame.
	handle(t, m, press(firefly.KeyS), release(firefly.KeyS))
	if !m.Pressed("jump") || !m.Released("jump") || m.Held("jump") {
		t.Fatal("a tap must produce both edges and no held state")
	}

	// A new frame clears the previous edges.
	handle(t, m)
	if m.Pressed("jump") || m.Released("jump") || m.Held("jump") {
		t.Fatal("edges must last exactly one frame")
	}

	// Two taps in the same frame collapse into boolean edges.
	handle(t, m,
		press(firefly.KeyS), release(firefly.KeyS),
		press(firefly.KeyS), release(firefly.KeyS),
	)
	if !m.Pressed("jump") || !m.Released("jump") || m.Held("jump") {
		t.Fatal("repeated taps in one frame must be idempotent")
	}

	// A tap followed by a hold ends held, and both edges are visible
	// because both transitions really happened in this frame.
	handle(t, m, press(firefly.KeyS), release(firefly.KeyS), press(firefly.KeyS))
	if !m.Pressed("jump") || !m.Released("jump") || !m.Held("jump") {
		t.Fatal("a tap followed by a hold must end held with both edges")
	}
}

func TestActionMap_UnknownKeys(t *testing.T) {
	t.Parallel()
	m := newMap(t)
	bind(t, m, "jump", firefly.KeyS)
	handle(t, m, press(firefly.KeyS))

	bad := firefly.Key(200)
	if err := m.Handle(press(firefly.KeyE), firefly.Event{Key: bad, Down: true}); !errors.Is(err, firefly.ErrUnknownKey) {
		t.Fatalf("want ErrUnknownKey, got %v", err)
	}
	// The invalid batch is rejected completely.
	if !m.Held("jump") || !m.Pressed("jump") {
		t.Fatal("a rejected batch must not change the state")
	}

	if m.Has("dash") {
		t.Fatal("dash should not exist yet")
	}
	if err := m.Define("dash", firefly.KeyN, firefly.KeyNone); !errors.Is(err, firefly.ErrUnknownKey) {
		t.Fatalf("want ErrUnknownKey, got %v", err)
	}
	if m.Has("dash") {
		t.Fatal("a failed Define must not create the action")
	}

	if _, err := firefly.ParseKey("Space"); !errors.Is(err, firefly.ErrUnknownKey) {
		t.Fatalf("want ErrUnknownKey, got %v", err)
	}
	if firefly.KeyNone.Valid() {
		t.Fatal("KeyNone must not be valid")
	}
}

func TestActionMap_InvalidActionNames(t *testing.T) {
	t.Parallel()
	m := newMap(t)
	for _, name := range []string{"", "has space", "a=b", "a,b", "#hash", strings.Repeat("x", 65)} {
		if err := m.Define(name); !errors.Is(err, firefly.ErrInvalidActionName) {
			t.Errorf("Define(%q): want ErrInvalidActionName, got %v", name, err)
		}
	}
	if !m.Has("jump") {
		t.Fatal("failed defines must not disturb existing actions")
	}
}

func TestActionMap_Unbind(t *testing.T) {
	t.Parallel()
	m := newMap(t)
	bind(t, m, "jump", firefly.KeyS)
	handle(t, m, press(firefly.KeyS))
	if !m.Held("jump") {
		t.Fatal("jump should be held")
	}

	// Removing a held action drops its state without producing edges.
	if !m.Unbind("jump") {
		t.Fatal("Unbind should report success")
	}
	if m.Unbind("jump") {
		t.Fatal("Unbind of an unknown action should return false")
	}
	if m.Held("jump") || m.Pressed("jump") || m.Released("jump") {
		t.Fatal("unbound action must not report state")
	}

	// Re-defining creates a fresh action; a held key is picked up
	// without a press edge.
	bind(t, m, "jump", firefly.KeyS)
	if !m.Held("jump") || m.Pressed("jump") {
		t.Fatal("re-defining should pick up the currently held key without an edge")
	}
}

func TestActionMap_Feed(t *testing.T) {
	t.Parallel()
	m := newMap(t)
	bind(t, m, "jump", firefly.KeyS)
	bind(t, m, "left", firefly.KeyDPadLeft)
	bind(t, m, "menu", firefly.KeyMenu)

	m.Feed(firefly.RawInput{Buttons: firefly.Buttons{S: true}})
	if !m.Pressed("jump") || !m.Held("jump") {
		t.Fatal("Feed should press jump")
	}
	if m.Held("left") {
		t.Fatal("untouched pad must not press any direction")
	}

	m.Feed(firefly.RawInput{Pad: firefly.Pad{X: -1000}, PadTouched: true})
	if !m.Released("jump") {
		t.Fatal("Feed should release jump when the button goes up")
	}
	if !m.Pressed("left") || !m.Held("left") {
		t.Fatal("Feed should press DPadLeft")
	}

	// A touched pad in the dead zone releases the direction.
	m.Feed(firefly.RawInput{Pad: firefly.Pad{X: -100}, PadTouched: true})
	if !m.Released("left") || m.Held("left") {
		t.Fatal("dead zone should release the direction")
	}

	m.Feed(firefly.RawInput{Pad: firefly.Pad{X: -1000}, PadTouched: true})
	if !m.Held("left") {
		t.Fatal("Feed should press DPadLeft again")
	}

	// Lifting the finger releases every direction.
	m.Feed(firefly.RawInput{Pad: firefly.Pad{X: -1000}, PadTouched: false})
	if !m.Released("left") || m.Held("left") {
		t.Fatal("an untouched pad should release all directions")
	}

	m.Feed(firefly.RawInput{Buttons: firefly.Buttons{Menu: true}})
	if !m.Pressed("menu") {
		t.Fatal("Feed should press the menu action")
	}
}

func TestActionMap_ReleaseAllOnZeroValue(t *testing.T) {
	t.Parallel()
	var m firefly.ActionMap
	m.ReleaseAll()
	if len(m.Actions()) != 0 {
		t.Fatal("ReleaseAll must not create actions")
	}
	bind(t, &m, "jump", firefly.KeyS)
	m.ReleaseAll()
	if m.Held("jump") || m.Pressed("jump") {
		t.Fatal("ReleaseAll on an idle map must be a no-op")
	}
}

func TestActionMap_ConfigRoundTrip(t *testing.T) {
	t.Parallel()
	m := newMap(t)
	bind(t, m, "jump", firefly.KeyE, firefly.KeyS)
	bind(t, m, "shoot", firefly.KeyN)
	bind(t, m, "left", firefly.KeyDPadLeft)
	bind(t, m, "empty")
	handle(t, m, press(firefly.KeyS))

	raw, err := m.MarshalText()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := strings.Join([]string{
		"firefly-actions v1",
		"confirm=",
		"empty=",
		"jump=S,E",
		"left=DPadLeft",
		"shoot=N",
		"",
	}, "\n")
	if string(raw) != want {
		t.Fatalf("unexpected config:\n%s\nwant:\n%s", raw, want)
	}

	restored := firefly.NewActionMap()
	if err := restored.UnmarshalText(raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got := restored.String(); got != want {
		t.Fatalf("restored config differs:\n%s\nwant:\n%s", got, want)
	}
	if restored.Held("jump") {
		t.Fatal("restore must not carry over transient input state")
	}

	// The restored map behaves like the original.
	handle(t, restored, press(firefly.KeyS))
	if !restored.Pressed("jump") || !restored.Held("jump") {
		t.Fatal("restored bindings should work")
	}
	if restored.Pressed("shoot") {
		t.Fatal("restored bindings must stay independent")
	}
}

func TestActionMap_ConfigParsing(t *testing.T) {
	t.Parallel()
	text := strings.Join([]string{
		"# a comment",
		"",
		"firefly-actions v1",
		"  jump = s , E  ",
		"jump2=s,s",
		"blocked=",
		"",
	}, "\n")
	m := firefly.NewActionMap()
	if err := m.UnmarshalText([]byte(text)); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	keys := m.Keys("jump")
	if len(keys) != 2 || keys[0] != firefly.KeyS || keys[1] != firefly.KeyE {
		t.Fatalf("unexpected keys for jump: %v", keys)
	}
	if len(m.Keys("jump2")) != 1 {
		t.Fatal("duplicate keys in config must be collapsed")
	}
	if len(m.Keys("blocked")) != 0 || !m.Has("blocked") {
		t.Fatal("an empty binding must create an unbound action")
	}

	// Surrounding whitespace is insignificant; blank lines are ignored.
	spaces := "  \nfirefly-actions v1\n\njump=S\n\n"
	m2 := firefly.NewActionMap()
	if err := m2.UnmarshalText([]byte(spaces)); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(m2.Keys("jump")) != 1 {
		t.Fatal("jump should be restored")
	}
}

func TestActionMap_BadConfig(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"empty":            "",
		"missing header":   "jump=S\n",
		"wrong header":     "firefly-actions v2\njump=S\n",
		"no equals":        "firefly-actions v1\njump\n",
		"unknown key":      "firefly-actions v1\njump=Space\n",
		"empty key":        "firefly-actions v1\njump=S,\n",
		"bad action name":  "firefly-actions v1\nhas space=S\n",
		"duplicate action": "firefly-actions v1\njump=S\njump=E\n",
		"invalid utf8":     "firefly-actions v1\njump=\xff\n",
	}

	for name, text := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			m := newMap(t)
			bind(t, m, "jump", firefly.KeyS)
			err := m.UnmarshalText([]byte(text))
			if err == nil {
				t.Fatal("want an error")
			}
			if !errors.Is(err, firefly.ErrInvalidActionConfig) {
				t.Fatalf("config errors must wrap ErrInvalidActionConfig: %v", err)
			}
			// The map is untouched on error.
			if !m.Has("jump") || len(m.Keys("jump")) != 1 || m.Keys("jump")[0] != firefly.KeyS {
				t.Fatal("a failed restore must not modify the map")
			}
		})
	}
}

func TestActionMap_ActionOrderIsStable(t *testing.T) {
	t.Parallel()
	m1 := firefly.NewActionMap()
	bind(t, m1, "z", firefly.KeyN)
	bind(t, m1, "a", firefly.KeyS)
	bind(t, m1, "m", firefly.KeyE)

	m2 := firefly.NewActionMap()
	bind(t, m2, "m", firefly.KeyE)
	bind(t, m2, "a", firefly.KeyS)
	bind(t, m2, "z", firefly.KeyN)

	if m1.String() != m2.String() {
		t.Fatalf("config must not depend on definition order:\n%s\n%s", m1, m2)
	}
	got := m1.Actions()
	if len(got) != 3 || got[0] != "a" || got[1] != "m" || got[2] != "z" {
		t.Fatalf("Actions must be sorted, got %v", got)
	}
}

func TestKey_Names(t *testing.T) {
	t.Parallel()
	for key := firefly.KeyS; key <= firefly.KeyDPadRight; key++ {
		name := key.String()
		parsed, err := firefly.ParseKey(name)
		if err != nil || parsed != key {
			t.Errorf("ParseKey(%q) = %v, %v; want %v", name, parsed, err, key)
		}
		if _, err := firefly.ParseKey(strings.ToLower(name)); err != nil {
			t.Errorf("ParseKey must be case-insensitive: %q: %v", name, err)
		}
	}
	if got := firefly.Key(200).String(); got != "Key(200)" {
		t.Errorf("unknown key should be numbered, got %q", got)
	}
	if got := firefly.KeyNone.String(); got != "None" {
		t.Errorf("KeyNone.String() = %q", got)
	}
	if _, err := firefly.ParseKey("None"); !errors.Is(err, firefly.ErrUnknownKey) {
		t.Errorf("KeyNone must have no parsable name, got %v", err)
	}
}

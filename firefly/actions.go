package firefly

import (
	"errors"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Key identifies a physical input that can be bound to an action.
//
// The button keys map directly to the fields of [Buttons]. The DPad keys are
// derived from [Pad] with the same thresholds as [Pad.DPad8].
type Key uint8

// All keys that can be used in an action binding.
const (
	// The zero value. It is not a valid key and cannot be bound.
	KeyNone Key = iota

	// The south button (A on an X-Box controller).
	KeyS

	// The east button (B on an X-Box controller).
	KeyE

	// The west button (X on an X-Box controller).
	KeyW

	// The north button (Y on an X-Box controller).
	KeyN

	// The menu button.
	KeyMenu

	// The top direction of the touch pad.
	KeyDPadUp

	// The bottom direction of the touch pad.
	KeyDPadDown

	// The left direction of the touch pad.
	KeyDPadLeft

	// The right direction of the touch pad.
	KeyDPadRight
)

// The maximum length of an action name, in bytes.
const maxActionNameLen = 64

// The header that must start a serialized action configuration.
const actionConfigHeader = "firefly-actions v1"

// Errors returned by [ActionMap] methods.
var (
	// The key is [KeyNone], out of range, or unknown to [ParseKey].
	ErrUnknownKey = errors.New("unknown key")

	// The action name is empty, too long, or contains a forbidden character.
	ErrInvalidActionName = errors.New("invalid action name")

	// A serialized action configuration cannot be parsed.
	ErrInvalidActionConfig = errors.New("invalid action config")
)

// Check if the key can be used in a binding.
func (k Key) Valid() bool {
	return k >= KeyS && k <= KeyDPadRight
}

// Get the canonical name of the key.
//
// The name is used by serialized action configurations.
func (k Key) String() string {
	switch k {
	case KeyNone:
		return "None"
	case KeyS:
		return "S"
	case KeyE:
		return "E"
	case KeyW:
		return "W"
	case KeyN:
		return "N"
	case KeyMenu:
		return "Menu"
	case KeyDPadUp:
		return "DPadUp"
	case KeyDPadDown:
		return "DPadDown"
	case KeyDPadLeft:
		return "DPadLeft"
	case KeyDPadRight:
		return "DPadRight"
	default:
		return "Key(" + strconv.Itoa(int(k)) + ")"
	}
}

// Parse the canonical name of a key.
//
// The name is matched case-insensitively. [KeyNone] has no valid name:
// ParseKey("None") returns [ErrUnknownKey].
func ParseKey(name string) (Key, error) {
	for k := KeyS; k <= KeyDPadRight; k++ {
		if strings.EqualFold(name, k.String()) {
			return k, nil
		}
	}
	return KeyNone, wrapErr(ErrUnknownKey, strconv.Quote(name))
}

// Event is a single physical input transition fed into an [ActionMap].
//
// Down is true when the key was pressed and false when it was released.
// Feeding only transitions keeps the result independent of the runtime,
// which makes the input layer fully testable.
type Event struct {
	Key  Key
	Down bool
}

// RawInput is a snapshot of all inputs that can be bound to actions.
//
// It is usually created with [ReadRawInput], but games can inject any
// snapshot they like. PadTouched has the same meaning as the second
// return value of [ReadPad]: if false, no DPad direction is pressed.
type RawInput struct {
	Buttons    Buttons
	Pad        Pad
	PadTouched bool
}

// ReadRawInput reads the buttons and the touch pad of the given peer.
//
// The peer can be [Combined] or one of the [GetPeers].
func ReadRawInput(p Peer) RawInput {
	pad, touched := ReadPad(p)
	return RawInput{
		Buttons:    ReadButtons(p),
		Pad:        pad,
		PadTouched: touched,
	}
}

// ActionMap maps game actions to one or more physical keys.
//
// An action is held while at least one of its bound keys is down.
// Several actions can share the same key: all of them are then driven
// by that key (broadcast). A duplicate key inside a single action is
// ignored. Rebinding takes effect immediately and never generates
// press or release edges by itself.
//
// The zero value is ready to use and contains no actions.
//
// The map is not safe for concurrent use, which matches the single-threaded
// nature of the Firefly runtime.
type ActionMap struct {
	bindings map[string][]Key
	down     map[Key]bool
	held     map[string]bool
	pressed  map[string]bool
	released map[string]bool
}

// NewActionMap creates an empty action map.
//
// The zero value of [ActionMap] can be used instead.
func NewActionMap() *ActionMap {
	return &ActionMap{}
}

func (m *ActionMap) ensure() {
	if m.bindings == nil {
		m.bindings = make(map[string][]Key)
	}
	if m.down == nil {
		m.down = make(map[Key]bool)
	}
	if m.held == nil {
		m.held = make(map[string]bool)
	}
	if m.pressed == nil {
		m.pressed = make(map[string]bool)
	}
	if m.released == nil {
		m.released = make(map[string]bool)
	}
}

// Define the keys bound to an action, replacing any previous bindings.
//
// The action is created if it doesn't exist yet. An action with no keys is
// allowed and simply never active. Duplicate keys are ignored.
//
// If the action is currently held and the new bindings make it inactive,
// [ActionMap.Held] becomes false immediately but no release edge is emitted.
//
// The method is atomic: if the name or any key is invalid, the map is
// not modified and an error is returned.
func (m *ActionMap) Define(action string, keys ...Key) error {
	if !validActionName(action) {
		return wrapErr(ErrInvalidActionName, strconv.Quote(action))
	}
	unique := make([]Key, 0, len(keys))
	seen := make(map[Key]bool, len(keys))
	for _, key := range keys {
		if !key.Valid() {
			return wrapErr(ErrUnknownKey, strconv.Itoa(int(key)))
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		unique = append(unique, key)
	}
	m.ensure()
	m.bindings[action] = unique
	m.recomputeHeld(action)
	return nil
}

// Bind is an alias for [ActionMap.Define].
//
// It is meant for rebinding an action at runtime, for example from a
// settings screen.
func (m *ActionMap) Bind(action string, keys ...Key) error {
	return m.Define(action, keys...)
}

// Remove an action and all its bindings.
//
// It returns false if the action did not exist.
func (m *ActionMap) Unbind(action string) bool {
	if m.bindings == nil {
		return false
	}
	if _, ok := m.bindings[action]; !ok {
		return false
	}
	delete(m.bindings, action)
	delete(m.held, action)
	delete(m.pressed, action)
	delete(m.released, action)
	return true
}

// Check if the action is defined.
func (m *ActionMap) Has(action string) bool {
	_, ok := m.bindings[action]
	return ok
}

// Get the keys bound to the action, in binding order.
//
// The order is the one passed to [ActionMap.Define], or the one read from
// the serialized configuration after [ActionMap.UnmarshalText].
//
// The returned slice is a copy and can be modified freely.
// If the action is unknown or unbound, nil is returned.
func (m *ActionMap) Keys(action string) []Key {
	keys := m.bindings[action]
	if len(keys) == 0 {
		return nil
	}
	out := make([]Key, len(keys))
	copy(out, keys)
	return out
}

// Get the names of all defined actions, sorted alphabetically.
func (m *ActionMap) Actions() []string {
	names := make([]string, 0, len(m.bindings))
	for name := range m.bindings {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// Check if the action became active during the current frame.
//
// An action becomes active when its first bound key goes down.
// Pressing an additional bound key while the action is already held
// does not produce another press edge.
func (m *ActionMap) Pressed(action string) bool {
	return m.pressed[action]
}

// Check if the action is active at the end of the current frame.
//
// An action is active while at least one of its bound keys is down.
func (m *ActionMap) Held(action string) bool {
	return m.held[action]
}

// Check if the action became inactive during the current frame.
//
// An action becomes inactive when its last bound key goes up.
// Releasing one of several bound keys while another one is still down
// does not produce a release edge.
func (m *ActionMap) Released(action string) bool {
	return m.released[action]
}

// Apply a sequence of input transitions and start a new frame.
//
// The call clears all press and release edges from the previous frame,
// then processes the events in order. A duplicate event (pressing an
// already pressed key or releasing an already released key) is ignored.
//
// If the same key is pressed and released within one frame, the action
// has both [ActionMap.Pressed] and [ActionMap.Released] set, and
// [ActionMap.Held] is false. Repeating such a tap in the same frame
// doesn't change the result: edges are booleans, not counters.
//
// If any event contains an invalid key, the whole batch is rejected and
// the map is left exactly as it was, including the edges of the previous
// frame. [ErrUnknownKey] is returned in this case.
func (m *ActionMap) Handle(events ...Event) error {
	for _, event := range events {
		if !event.Key.Valid() {
			return wrapErr(ErrUnknownKey, strconv.Itoa(int(event.Key)))
		}
	}
	m.beginFrame()
	for _, event := range events {
		m.setDown(event.Key, event.Down)
	}
	return nil
}

// Apply a raw input snapshot and start a new frame.
//
// The map compares the snapshot with the keys that are currently down and
// generates the corresponding transitions. This is the recommended way to
// drive actions from the normal input API:
//
//	actions.Feed(firefly.ReadRawInput(firefly.Combined))
//
// Pad directions are only read when PadTouched is true.
//
// Don't mix Feed and [ActionMap.Handle] for the same frame: the last call
// starts the frame and only its edges are visible.
func (m *ActionMap) Feed(in RawInput) {
	m.beginFrame()
	m.setDown(KeyS, in.Buttons.S)
	m.setDown(KeyE, in.Buttons.E)
	m.setDown(KeyW, in.Buttons.W)
	m.setDown(KeyN, in.Buttons.N)
	m.setDown(KeyMenu, in.Buttons.Menu)

	dpad := DPad8{}
	if in.PadTouched {
		dpad = in.Pad.DPad8()
	}
	m.setDown(KeyDPadUp, dpad.Up)
	m.setDown(KeyDPadDown, dpad.Down)
	m.setDown(KeyDPadLeft, dpad.Left)
	m.setDown(KeyDPadRight, dpad.Right)
}

// Release every key and start a new frame.
//
// Call it when the app loses focus or is suspended, so that an action
// can't stay held forever. Every active action gets a release edge.
// Calling it when no key is down just clears the previous edges.
func (m *ActionMap) ReleaseAll() {
	m.beginFrame()
	for key := KeyS; key <= KeyDPadRight; key++ {
		m.setDown(key, false)
	}
}

// Serialize the action bindings into a stable text configuration.
//
// Actions are sorted by name, keys are sorted by their numeric value,
// and a trailing newline is added. The transient input state (which keys
// are down and the current edges) is not serialized.
func (m *ActionMap) MarshalText() ([]byte, error) {
	return []byte(m.configString()), nil
}

// Restore action bindings from a configuration created by
// [ActionMap.MarshalText].
//
// The configuration is validated in full before anything is changed.
// On error the map is left untouched. On success all transient input
// state is reset: every key is considered released, so no action is
// held and no edges are set.
//
// The format is:
//
//	firefly-actions v1
//	jump=S,W
//	shoot=DPadLeft
//	unpaired=
//
// Blank lines and lines starting with '#' are ignored. Key names are
// case-insensitive. Every action may appear only once. An action with
// no keys after '=' is valid and unbound.
//
// Any error returned by this method wraps [ErrInvalidActionConfig].
// The detail in the message tells whether the problem is a bad header,
// an unknown key, a duplicate action, or an invalid action name.
func (m *ActionMap) UnmarshalText(data []byte) error {
	bindings, err := parseActionConfig(string(data))
	if err != nil {
		return err
	}
	m.bindings = bindings
	m.down = make(map[Key]bool)
	m.held = make(map[string]bool)
	m.pressed = make(map[string]bool)
	m.released = make(map[string]bool)
	return nil
}

// Get the serialized configuration as a string.
func (m *ActionMap) String() string {
	return m.configString()
}

func (m *ActionMap) configString() string {
	var buf strings.Builder
	buf.WriteString(actionConfigHeader)
	buf.WriteByte('\n')
	for _, action := range m.Actions() {
		keys := slices.Clone(m.bindings[action])
		slices.Sort(keys)
		buf.WriteString(action)
		buf.WriteByte('=')
		for i, key := range keys {
			if i != 0 {
				buf.WriteByte(',')
			}
			buf.WriteString(key.String())
		}
		buf.WriteByte('\n')
	}
	return buf.String()
}

func (m *ActionMap) beginFrame() {
	m.ensure()
	clear(m.pressed)
	clear(m.released)
}

func (m *ActionMap) setDown(key Key, down bool) {
	if m.down[key] == down {
		return
	}
	if down {
		m.down[key] = true
	} else {
		delete(m.down, key)
	}
	for action, keys := range m.bindings {
		for _, bound := range keys {
			if bound == key {
				m.refreshHeld(action, keys)
				break
			}
		}
	}
}

// Update the held state of an action and emit an edge if it changed.
func (m *ActionMap) refreshHeld(action string, keys []Key) {
	now := false
	for _, key := range keys {
		if m.down[key] {
			now = true
			break
		}
	}
	if now == m.held[action] {
		return
	}
	m.held[action] = now
	if now {
		m.pressed[action] = true
	} else {
		m.released[action] = true
	}
}

// Update the held state of an action without emitting an edge.
func (m *ActionMap) recomputeHeld(action string) {
	now := false
	for _, key := range m.bindings[action] {
		if m.down[key] {
			now = true
			break
		}
	}
	m.held[action] = now
}

func parseActionConfig(text string) (map[string][]Key, error) {
	bindings := make(map[string][]Key)
	headerSeen := false
	for i, rawLine := range strings.Split(text, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !headerSeen {
			if line != actionConfigHeader {
				return nil, wrapErr(
					ErrInvalidActionConfig,
					"expected "+strconv.Quote(actionConfigHeader)+lineSuffix(i),
				)
			}
			headerSeen = true
			continue
		}
		action, keys, err := parseActionLine(line)
		if err != nil {
			return nil, wrapErr(ErrInvalidActionConfig, err.Error()+lineSuffix(i))
		}
		if _, duplicate := bindings[action]; duplicate {
			return nil, wrapErr(
				ErrInvalidActionConfig,
				"duplicate action "+strconv.Quote(action)+lineSuffix(i),
			)
		}
		bindings[action] = keys
	}
	if !headerSeen {
		return nil, wrapErr(ErrInvalidActionConfig, "missing "+strconv.Quote(actionConfigHeader))
	}
	return bindings, nil
}

func parseActionLine(line string) (string, []Key, error) {
	action, rawKeys, ok := strings.Cut(line, "=")
	if !ok {
		return "", nil, wrapErr(ErrInvalidActionConfig, "missing '='")
	}
	action = strings.TrimSpace(action)
	if !validActionName(action) {
		return "", nil, wrapErr(ErrInvalidActionName, strconv.Quote(action))
	}
	rawKeys = strings.TrimSpace(rawKeys)
	keys := make([]Key, 0, 4)
	if rawKeys == "" {
		return action, keys, nil
	}
	seen := make(map[Key]bool)
	for _, rawKey := range strings.Split(rawKeys, ",") {
		key, err := ParseKey(strings.TrimSpace(rawKey))
		if err != nil {
			return "", nil, err
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		keys = append(keys, key)
	}
	return action, keys, nil
}

// lineSuffix formats the 0-based line index as a human-readable suffix.
func lineSuffix(index int) string {
	return " at line " + strconv.Itoa(index+1)
}

// wrapErr attaches a sentinel error to a short human-readable detail
// without pulling [fmt] (and its reflection machinery) into the wasm binary.
func wrapErr(kind error, detail string) error {
	return actionError{kind: kind, detail: detail}
}

type actionError struct {
	kind   error
	detail string
}

func (e actionError) Error() string {
	return e.kind.Error() + ": " + e.detail
}

func (e actionError) Unwrap() error {
	return e.kind
}

func validActionName(name string) bool {
	if name == "" || len(name) > maxActionNameLen || !utf8.ValidString(name) {
		return false
	}
	for _, r := range name {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return false
		}
		switch r {
		case '=', ',', '#':
			return false
		}
	}
	return true
}

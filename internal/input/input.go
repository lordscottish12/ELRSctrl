// Package input defines the set of mappable gamepad controls ("sources") and a
// normalized snapshot of their current values, decoupled from Ebiten so the
// mapping engine and tests don't depend on a window being open.
package input

// Source identifies one mappable control on the gamepad.
type Source string

const (
	SrcNone Source = "none"

	// Analog axes, bipolar (-1..1, 0 = center). Note: Ebiten reports stick
	// vertical axes with up = -1, down = +1; reverse per-channel if desired.
	SrcLeftStickX  Source = "left_stick_x"
	SrcLeftStickY  Source = "left_stick_y"
	SrcRightStickX Source = "right_stick_x"
	SrcRightStickY Source = "right_stick_y"

	// Analog triggers, unipolar (0..1).
	SrcLeftTrigger  Source = "left_trigger"  // L2
	SrcRightTrigger Source = "right_trigger" // R2

	// Combined throttle axis, bipolar: R2 forward (+1) / L2 reverse (-1).
	// Ideal default for an RC car's throttle channel.
	SrcTriggers Source = "triggers"

	// Digital buttons.
	SrcA      Source = "a"
	SrcB      Source = "b"
	SrcX      Source = "x"
	SrcY      Source = "y"
	SrcLB     Source = "lb" // left bumper / L1
	SrcRB     Source = "rb" // right bumper / R1
	SrcL3     Source = "l3" // left stick click
	SrcR3     Source = "r3" // right stick click
	SrcView   Source = "view"   // back / select
	SrcMenu   Source = "menu"   // start
	SrcSteam  Source = "steam"  // guide / steam button
	SrcDUp    Source = "dpad_up"
	SrcDDown  Source = "dpad_down"
	SrcDLeft  Source = "dpad_left"
	SrcDRight Source = "dpad_right"
)

// AnalogSources lists every analog source, in UI-display order.
var AnalogSources = []Source{
	SrcLeftStickX, SrcLeftStickY, SrcRightStickX, SrcRightStickY,
	SrcLeftTrigger, SrcRightTrigger, SrcTriggers,
}

// ButtonSources lists every digital source, in UI-display order.
var ButtonSources = []Source{
	SrcA, SrcB, SrcX, SrcY, SrcLB, SrcRB, SrcL3, SrcR3,
	SrcView, SrcMenu, SrcSteam, SrcDUp, SrcDDown, SrcDLeft, SrcDRight,
}

// AllSources is every selectable source including "none", for UI steppers.
var AllSources = func() []Source {
	out := []Source{SrcNone}
	out = append(out, AnalogSources...)
	out = append(out, ButtonSources...)
	return out
}()

var labels = map[Source]string{
	SrcNone:         "None",
	SrcLeftStickX:   "Left Stick X",
	SrcLeftStickY:   "Left Stick Y",
	SrcRightStickX:  "Right Stick X",
	SrcRightStickY:  "Right Stick Y",
	SrcLeftTrigger:  "Left Trigger (L2)",
	SrcRightTrigger: "Right Trigger (R2)",
	SrcTriggers:     "Triggers (R2 fwd / L2 rev)",
	SrcA:            "A",
	SrcB:            "B",
	SrcX:            "X",
	SrcY:            "Y",
	SrcLB:           "Left Bumper (L1)",
	SrcRB:           "Right Bumper (R1)",
	SrcL3:           "Left Stick Click",
	SrcR3:           "Right Stick Click",
	SrcView:         "View (Back)",
	SrcMenu:         "Menu (Start)",
	SrcSteam:        "Steam",
	SrcDUp:          "D-Pad Up",
	SrcDDown:        "D-Pad Down",
	SrcDLeft:        "D-Pad Left",
	SrcDRight:       "D-Pad Right",
}

// Label returns a human-readable name for a source.
func (s Source) Label() string {
	if l, ok := labels[s]; ok {
		return l
	}
	return string(s)
}

// IsAnalog reports whether the source produces a continuous value.
func (s Source) IsAnalog() bool {
	for _, a := range AnalogSources {
		if a == s {
			return true
		}
	}
	return false
}

// IsButton reports whether the source is a digital button.
func (s Source) IsButton() bool {
	for _, b := range ButtonSources {
		if b == s {
			return true
		}
	}
	return false
}

// Bipolar reports whether an analog source ranges -1..1 (vs. 0..1 unipolar).
func (s Source) Bipolar() bool {
	switch s {
	case SrcLeftTrigger, SrcRightTrigger:
		return false
	default:
		return true
	}
}

// State is a normalized snapshot of all gamepad controls for one frame.
type State struct {
	Connected bool
	Name      string
	Axes      map[Source]float64 // bipolar in -1..1, unipolar triggers in 0..1
	Buttons   map[Source]bool
}

// NewState returns an empty (disconnected) state with initialized maps.
func NewState() State {
	return State{Axes: map[Source]float64{}, Buttons: map[Source]bool{}}
}

// Analog returns the current value of an analog source (0 if absent).
func (s State) Analog(src Source) float64 { return s.Axes[src] }

// Pressed reports whether a button source is currently held.
func (s State) Pressed(src Source) bool { return s.Buttons[src] }

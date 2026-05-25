package input

import "github.com/hajimehoshi/ebiten/v2"

// Device describes a connected gamepad for selection in the UI.
type Device struct {
	ID       ebiten.GamepadID
	Name     string
	Standard bool // has an SDL standard-layout mapping
}

// List returns the gamepads Ebiten currently sees. It must be called from
// within the Ebiten game loop (Update), like all gamepad queries.
func List() []Device {
	var ids []ebiten.GamepadID
	ids = ebiten.AppendGamepadIDs(ids)
	out := make([]Device, 0, len(ids))
	for _, id := range ids {
		out = append(out, Device{
			ID:       id,
			Name:     ebiten.GamepadName(id),
			Standard: ebiten.IsStandardGamepadLayoutAvailable(id),
		})
	}
	return out
}

// Reader reads a selected gamepad each frame into a normalized State.
type Reader struct {
	selected ebiten.GamepadID
	hasSel   bool
}

// Select pins the reader to a specific gamepad ID.
func (r *Reader) Select(id ebiten.GamepadID) {
	r.selected = id
	r.hasSel = true
}

// Auto clears the pinned gamepad so Poll falls back to the first available one.
func (r *Reader) Auto() { r.hasSel = false }

// Selected returns the pinned gamepad ID and whether one is set.
func (r *Reader) Selected() (ebiten.GamepadID, bool) { return r.selected, r.hasSel }

// resolve picks the gamepad to read: the pinned one if still present,
// otherwise the first available (auto-select).
func (r *Reader) resolve() (ebiten.GamepadID, bool) {
	devs := List()
	if r.hasSel {
		for _, d := range devs {
			if d.ID == r.selected {
				return r.selected, true
			}
		}
	}
	if len(devs) > 0 {
		return devs[0].ID, true
	}
	return 0, false
}

// Poll reads the current gamepad state. Must be called from Update.
func (r *Reader) Poll() State {
	st := NewState()
	id, ok := r.resolve()
	if !ok {
		return st
	}
	st.Connected = true
	st.Name = ebiten.GamepadName(id)

	if ebiten.IsStandardGamepadLayoutAvailable(id) {
		r.pollStandard(id, &st)
	} else {
		r.pollRaw(id, &st)
	}
	return st
}

func (r *Reader) pollStandard(id ebiten.GamepadID, st *State) {
	axis := func(a ebiten.StandardGamepadAxis) float64 {
		return ebiten.StandardGamepadAxisValue(id, a)
	}
	st.Axes[SrcLeftStickX] = axis(ebiten.StandardGamepadAxisLeftStickHorizontal)
	st.Axes[SrcLeftStickY] = axis(ebiten.StandardGamepadAxisLeftStickVertical)
	st.Axes[SrcRightStickX] = axis(ebiten.StandardGamepadAxisRightStickHorizontal)
	st.Axes[SrcRightStickY] = axis(ebiten.StandardGamepadAxisRightStickVertical)

	lt := ebiten.StandardGamepadButtonValue(id, ebiten.StandardGamepadButtonFrontBottomLeft)
	rt := ebiten.StandardGamepadButtonValue(id, ebiten.StandardGamepadButtonFrontBottomRight)
	st.Axes[SrcLeftTrigger] = lt
	st.Axes[SrcRightTrigger] = rt
	st.Axes[SrcTriggers] = rt - lt // R2 forward, L2 reverse

	btn := func(b ebiten.StandardGamepadButton) bool {
		return ebiten.IsStandardGamepadButtonPressed(id, b)
	}
	st.Buttons[SrcA] = btn(ebiten.StandardGamepadButtonRightBottom)
	st.Buttons[SrcB] = btn(ebiten.StandardGamepadButtonRightRight)
	st.Buttons[SrcX] = btn(ebiten.StandardGamepadButtonRightLeft)
	st.Buttons[SrcY] = btn(ebiten.StandardGamepadButtonRightTop)
	st.Buttons[SrcLB] = btn(ebiten.StandardGamepadButtonFrontTopLeft)
	st.Buttons[SrcRB] = btn(ebiten.StandardGamepadButtonFrontTopRight)
	st.Buttons[SrcL3] = btn(ebiten.StandardGamepadButtonLeftStick)
	st.Buttons[SrcR3] = btn(ebiten.StandardGamepadButtonRightStick)
	st.Buttons[SrcView] = btn(ebiten.StandardGamepadButtonCenterLeft)
	st.Buttons[SrcMenu] = btn(ebiten.StandardGamepadButtonCenterRight)
	st.Buttons[SrcSteam] = btn(ebiten.StandardGamepadButtonCenterCenter)
	st.Buttons[SrcDUp] = btn(ebiten.StandardGamepadButtonLeftTop)
	st.Buttons[SrcDDown] = btn(ebiten.StandardGamepadButtonLeftBottom)
	st.Buttons[SrcDLeft] = btn(ebiten.StandardGamepadButtonLeftLeft)
	st.Buttons[SrcDRight] = btn(ebiten.StandardGamepadButtonLeftRight)
}

// pollRaw is a best-effort fallback for gamepads without a standard layout:
// map the first axes/buttons positionally so the app is still usable.
func (r *Reader) pollRaw(id ebiten.GamepadID, st *State) {
	ac := ebiten.GamepadAxisCount(id)
	rawAxis := func(i int) float64 {
		if i < ac {
			return ebiten.GamepadAxisValue(id, i)
		}
		return 0
	}
	st.Axes[SrcLeftStickX] = rawAxis(0)
	st.Axes[SrcLeftStickY] = rawAxis(1)
	st.Axes[SrcRightStickX] = rawAxis(2)
	st.Axes[SrcRightStickY] = rawAxis(3)
	// Many pads expose triggers as axes 4/5 in 0..1 (or -1..1); pass through.
	lt := rawAxis(4)
	rt := rawAxis(5)
	st.Axes[SrcLeftTrigger] = lt
	st.Axes[SrcRightTrigger] = rt
	st.Axes[SrcTriggers] = rt - lt

	bc := ebiten.GamepadButtonCount(id)
	order := []Source{SrcA, SrcB, SrcX, SrcY, SrcLB, SrcRB, SrcView, SrcMenu,
		SrcSteam, SrcL3, SrcR3, SrcDUp, SrcDDown, SrcDLeft, SrcDRight}
	for i, src := range order {
		if i < bc {
			st.Buttons[src] = ebiten.IsGamepadButtonPressed(id, ebiten.GamepadButton(i))
		}
	}
}

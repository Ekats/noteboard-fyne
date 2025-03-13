package main

import (
	"fmt"
	"image/color"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

// KeyCaptureWidget is a custom widget that implements desktop.Keyable
type KeyCaptureWidget struct {
	widget.BaseWidget

	// Key combination tracking
	keyCombo      []string
	onKeysChanged func([]string)

	// State
	isFocused bool
	rect      *canvas.Rectangle
}

// NewKeyCaptureWidget creates a new key capture widget
func NewKeyCaptureWidget(onKeysChanged func([]string)) *KeyCaptureWidget {
	w := &KeyCaptureWidget{
		onKeysChanged: onKeysChanged,
		rect:          canvas.NewRectangle(color.NRGBA{R: 220, G: 220, B: 255, A: 255}),
	}
	w.ExtendBaseWidget(w)
	return w
}

// CreateRenderer creates a renderer for this widget
func (w *KeyCaptureWidget) CreateRenderer() fyne.WidgetRenderer {
	text := canvas.NewText("Click here and press keys...", color.NRGBA{R: 0, G: 0, B: 0, A: 255})
	text.Alignment = fyne.TextAlignCenter
	text.TextSize = 14

	return &keyCaptureWidgetRenderer{
		widget:  w,
		rect:    w.rect,
		text:    text,
		objects: []fyne.CanvasObject{w.rect, text},
	}
}

// keyCaptureWidgetRenderer is the renderer for KeyCaptureWidget
type keyCaptureWidgetRenderer struct {
	widget  *KeyCaptureWidget
	rect    *canvas.Rectangle
	text    *canvas.Text
	objects []fyne.CanvasObject
}

func (r *keyCaptureWidgetRenderer) Destroy() {}

func (r *keyCaptureWidgetRenderer) Layout(size fyne.Size) {
	r.rect.Resize(size)
	r.text.Resize(size)
}

func (r *keyCaptureWidgetRenderer) MinSize() fyne.Size {
	return fyne.NewSize(200, 40)
}

func (r *keyCaptureWidgetRenderer) Objects() []fyne.CanvasObject {
	return r.objects
}

func (r *keyCaptureWidgetRenderer) Refresh() {
	if r.widget.isFocused {
		r.rect.FillColor = color.NRGBA{R: 200, G: 200, B: 255, A: 255}
		r.text.Text = "Press keys now..."
	} else {
		r.rect.FillColor = color.NRGBA{R: 220, G: 220, B: 220, A: 255}
		r.text.Text = "Click here and press keys..."
	}

	r.rect.Refresh()
	r.text.Refresh()
}

// FocusGained is called when this widget gains focus
func (w *KeyCaptureWidget) FocusGained() {
	w.isFocused = true
	w.keyCombo = nil
	if w.onKeysChanged != nil {
		w.onKeysChanged(w.keyCombo)
	}
	w.Refresh()
}

// FocusLost is called when this widget loses focus
func (w *KeyCaptureWidget) FocusLost() {
	w.isFocused = false
	w.Refresh()
}

// TypedRune receives text input events when this widget is focused
func (w *KeyCaptureWidget) TypedRune(r rune) {
	// Add the character to the key combo if not already present
	keyStr := string(r)
	if !containsKey(w.keyCombo, keyStr) {
		w.keyCombo = append(w.keyCombo, keyStr)
		w.notifyKeysChanged()
	}
}

// TypedKey receives key input events when this widget is focused
func (w *KeyCaptureWidget) TypedKey(ke *fyne.KeyEvent) {
	keyName := getKeyName(ke.Name)
	if keyName != "" && !containsKey(w.keyCombo, keyName) {
		w.keyCombo = append(w.keyCombo, keyName)
		w.notifyKeysChanged()
	}
}

// KeyDown receives key down events when this widget is focused
func (w *KeyCaptureWidget) KeyDown(ke *fyne.KeyEvent) {
	keyName := getKeyName(ke.Name)
	if keyName != "" && !containsKey(w.keyCombo, keyName) {
		w.keyCombo = append(w.keyCombo, keyName)
		w.notifyKeysChanged()
	}
}

// KeyUp receives key up events when this widget is focused
func (w *KeyCaptureWidget) KeyUp(ke *fyne.KeyEvent) {
	// Don't remove keys on key up, we want to build a combination
}

// Tapped handles tap events to gain focus
func (w *KeyCaptureWidget) Tapped(*fyne.PointEvent) {
	fyne.CurrentApp().Driver().CanvasForObject(w).Focus(w)
}

// Notify listeners about key changes
func (w *KeyCaptureWidget) notifyKeysChanged() {
	if w.onKeysChanged != nil {
		sortKeysForHotkey(w.keyCombo)
		w.onKeysChanged(w.keyCombo)
	}
}

// Reset the captured keys
func (w *KeyCaptureWidget) Reset() {
	w.keyCombo = nil
	if w.onKeysChanged != nil {
		w.onKeysChanged(w.keyCombo)
	}
}

// Helper function to get a readable key name
func getKeyName(keyName fyne.KeyName) string {
	// Map special keys to more readable names
	switch keyName {
	case fyne.KeyEscape:
		return "esc"
	case fyne.KeyReturn:
		return "enter"
	case fyne.KeyTab:
		return "tab"
	case fyne.KeyBackspace:
		return "backspace"
	case fyne.KeyInsert:
		return "insert"
	case fyne.KeyDelete:
		return "delete"
	case fyne.KeyRight:
		return "right"
	case fyne.KeyLeft:
		return "left"
	case fyne.KeyDown:
		return "down"
	case fyne.KeyUp:
		return "up"
	case fyne.KeyHome:
		return "home"
	case fyne.KeyEnd:
		return "end"
	case fyne.KeyPageUp:
		return "pageup"
	case fyne.KeyPageDown:
		return "pagedown"
	case fyne.KeySpace:
		return "space"

	// Specific modifier keys from the documentation
	case desktop.KeyShiftLeft, desktop.KeyShiftRight:
		return "shift"
	case desktop.KeyControlLeft, desktop.KeyControlRight:
		return "ctrl"
	case desktop.KeyAltLeft, desktop.KeyAltRight:
		return "alt"
	case desktop.KeySuperLeft, desktop.KeySuperRight:
		return "super"
	case desktop.KeyCapsLock:
		return "capslock"
	case desktop.KeyMenu:
		return "menu"
	case desktop.KeyPrintScreen:
		return "printscreen"

	// Function keys
	case fyne.KeyF1, fyne.KeyF2, fyne.KeyF3, fyne.KeyF4, fyne.KeyF5, fyne.KeyF6,
		fyne.KeyF7, fyne.KeyF8, fyne.KeyF9, fyne.KeyF10, fyne.KeyF11, fyne.KeyF12:
		return string(keyName)
	}

	// For regular character keys, use the key name as is
	if len(string(keyName)) == 1 {
		return string(keyName)
	}

	return string(keyName)
}

// Helper function to check if a slice contains a string
func containsKey(slice []string, str string) bool {
	for _, v := range slice {
		if v == str {
			return true
		}
	}
	return false
}

// Sort keys with modifiers first, then regular keys
func sortKeysForHotkey(keys []string) {
	// Define the sort order for modifiers
	modifierOrder := map[string]int{
		"ctrl":  0,
		"alt":   1,
		"shift": 2,
		"super": 3,
	}

	// Sort the keys
	sort.Slice(keys, func(i, j int) bool {
		// Get order value for each key (default to a high number for non-modifiers)
		iValue, iIsModifier := modifierOrder[keys[i]]
		if !iIsModifier {
			iValue = 100
		}

		jValue, jIsModifier := modifierOrder[keys[j]]
		if !jIsModifier {
			jValue = 100
		}

		// Sort by order value, putting modifiers first
		if iValue != jValue {
			return iValue < jValue
		}

		// If same type (both modifiers or both not), sort alphabetically
		return keys[i] < keys[j]
	})
}

// CreateHotkeyDetector creates and returns a UI component for detecting hotkey combinations
func CreateHotkeyDetector(settingsWindow fyne.Window, cm *ClipboardManager) *fyne.Container {
	// Display different UI depending on whether we're on Wayland or X11
	if cm.isWayland {
		return createWaylandHotkeySettings(settingsWindow, cm)
	}

	return createX11HotkeyDetector(settingsWindow, cm)
}

// Create Wayland-specific hotkey settings that integrate with KDE
func createWaylandHotkeySettings(settingsWindow fyne.Window, cm *ClipboardManager) *fyne.Container {
	instructions := widget.NewLabel("On Wayland, global hotkeys need to be set using KDE System Settings.")

	detailedInstructions := widget.NewRichTextFromMarkdown(`
1. Open KDE System Settings
2. Go to Shortcuts → Custom Shortcuts
3. Click "Edit" → "New" → "Global Shortcut" → "Command/URL"
4. Name it "Clipboard Manager"
5. Set the command to the path of this application
6. Click "Trigger" tab and set your preferred keyboard shortcut
7. Click "Apply"
`)

	// Use the settingsWindow param for the dialog parent
	openSettingsButton := widget.NewButton("Open KDE Shortcuts Settings", func() {
		// Try to open KDE System Settings at the shortcuts page
		cmd := "kcmshell5 keys || systemsettings5 keys || systemsettings5 shortcuts"
		go func() {
			err := exec.Command("sh", "-c", cmd).Start()
			if err != nil {
				// Display error dialog using the settingsWindow param
				dialog.ShowError(fmt.Errorf("failed to open kde settings: %v", err), settingsWindow)
			}
		}()
	})

	// Use the cm param to get executable path
	execPath, err := os.Executable()
	execPathLabel := widget.NewLabel("Application path: Unknown")

	if err == nil {
		execPathLabel.SetText("Application path: " + execPath)
	} else {
		execPathLabel.SetText("Error getting path: " + err.Error())
	}

	// Create an info button that shows current shortcut config
	infoButton := widget.NewButton("Show Current Shortcut Info", func() {
		modifierKey := cm.hotkeySettings.ModifierKey
		actionKey := cm.hotkeySettings.ActionKey

		var message string
		if modifierKey != "" && actionKey != "" {
			message = fmt.Sprintf("Current shortcut configuration: %s+%s\n\nThis will be used when setting up the KDE shortcut.",
				modifierKey, actionKey)
		} else if actionKey != "" {
			message = fmt.Sprintf("Current shortcut configuration: %s\n\nThis will be used when setting up the KDE shortcut.",
				actionKey)
		} else {
			message = "No shortcut is currently configured. Please set one in KDE System Settings."
		}

		dialog.ShowInformation("Shortcut Configuration", message, settingsWindow)
	})

	return container.NewVBox(
		widget.NewLabel("Wayland Hotkeys Configuration"),
		instructions,
		detailedInstructions,
		execPathLabel,
		infoButton,
		openSettingsButton,
	)
}

// Create the original X11 hotkey detector UI
func createX11HotkeyDetector(settingsWindow fyne.Window, cm *ClipboardManager) *fyne.Container {
	keyDisplay := widget.NewEntry()
	keyDisplay.SetPlaceHolder("Hotkey will appear here...")
	keyDisplay.Disable() // Make it read-only

	// Create a new key capture widget
	var currentKeyCombo []string
	keyCaptureWidget := NewKeyCaptureWidget(func(keys []string) {
		currentKeyCombo = keys
		if len(keys) > 0 {
			keyDisplay.SetText(strings.Join(keys, "+"))
		} else {
			keyDisplay.SetText("")
		}
	})

	// Add buttons for actions
	resetButton := widget.NewButton("Reset", func() {
		keyCaptureWidget.Reset()
	})

	applyButton := widget.NewButton("Apply", func() {
		if len(currentKeyCombo) > 0 {
			// Sort keys so modifiers come first
			sortKeysForHotkey(currentKeyCombo)

			// Separate the last key as the action key (if there are multiple keys)
			var actionKey string
			var modifierKeys string

			if len(currentKeyCombo) > 1 {
				// Take the last key as action key
				actionKey = currentKeyCombo[len(currentKeyCombo)-1]
				// Join the rest as modifier keys
				modifierKeys = strings.Join(currentKeyCombo[:len(currentKeyCombo)-1], "+")
			} else {
				// If only one key is pressed, it's the action key
				actionKey = currentKeyCombo[0]
				modifierKeys = ""
			}

			// Update the hotkey settings
			cm.UpdateHotkey(modifierKeys, actionKey)

			// Construct message based on whether we have modifiers
			var message string
			if modifierKeys != "" {
				message = fmt.Sprintf("Hotkey set to %s+%s", modifierKeys, actionKey)
			} else {
				message = fmt.Sprintf("Hotkey set to %s", actionKey)
			}

			dialog.ShowInformation("Hotkey Updated",
				message+"\nRestart the application for changes to take effect.",
				settingsWindow)
		} else {
			dialog.ShowInformation("Invalid Hotkey",
				"Please press at least one key combination",
				settingsWindow)
		}
	})

	buttonContainer := container.NewHBox(applyButton, resetButton)

	// Instructions label
	instructions := widget.NewLabel("Click in the box below and press your desired key combination.")

	// Return the entire component
	return container.NewVBox(
		instructions,
		keyCaptureWidget,
		keyDisplay,
		buttonContainer,
	)
}

// ShowSettingsDialog creates and displays the settings window with the hotkey detector
func ShowSettingsDialog(a fyne.App, cm *ClipboardManager) {
	settingsWindow := a.NewWindow("Settings")
	settingsWindow.Resize(fyne.NewSize(400, 300))

	// Hotkey settings
	hotkeyLabel := widget.NewLabel("Hotkey Settings")
	hotkeyLabel.TextStyle = fyne.TextStyle{Bold: true}

	// Current hotkey display
	var currentHotkeyText string

	if cm.isWayland {
		currentHotkeyText = "Set in KDE System Settings"
	} else {
		currentHotkeyText = cm.hotkeySettings.ModifierKey
		if currentHotkeyText != "" && cm.hotkeySettings.ActionKey != "" {
			currentHotkeyText += "+"
		}
		currentHotkeyText += cm.hotkeySettings.ActionKey
	}

	currentHotkeyLabel := widget.NewLabel(fmt.Sprintf("Current hotkey: %s", currentHotkeyText))

	// Use the hotkey detector
	hotkeyDetector := CreateHotkeyDetector(settingsWindow, cm)

	// Other settings
	historyToggle := widget.NewCheck("Save history", func(bool) {})
	historyToggle.SetChecked(true)
	clearHistoryButton := widget.NewButton("Clear clipboard history", cm.clearItems)

	// Add autostart option
	autostartToggle := widget.NewCheck("Start on boot", func(checked bool) {
		if checked {
			err := createDesktopFile()
			if err != nil {
				dialog.ShowError(err, settingsWindow)
			}
		} else {
			// Remove autostart file if unchecked
			homeDir, err := os.UserHomeDir()
			if err == nil {
				autostartPath := filepath.Join(homeDir, ".config", "autostart", "manjaro-clipboard.desktop")
				os.Remove(autostartPath)
			}
		}
	})

	// Check if autostart file exists
	homeDir, _ := os.UserHomeDir()
	if homeDir != "" {
		autostartPath := filepath.Join(homeDir, ".config", "autostart", "manjaro-clipboard.desktop")
		if _, err := os.Stat(autostartPath); err == nil {
			autostartToggle.SetChecked(true)
		}
	}

	// KDE-specific settings
	var kdeOptions *fyne.Container
	if isKDEPlasma() {
		// Add "Keep Above Others" toggle for KDE
		keepAboveToggle := widget.NewCheck("Keep window above others (overlay mode)", func(checked bool) {
			go func() {
				err := cm.setKDEWindowKeepAbove(checked)
				if err != nil {
					dialog.ShowError(fmt.Errorf("failed to change keep above setting: %v", err), settingsWindow)
				} else {
					// Apply the change to current window as well
					if checked {
						dialog.ShowInformation("Success", "Window will now stay above others. Changes will apply after restart.", settingsWindow)
					} else {
						dialog.ShowInformation("Success", "Window will no longer stay above others. Changes will apply after restart.", settingsWindow)
					}
				}
			}()
		})
		// Set initial toggle state based on current setting
		keepAboveToggle.SetChecked(cm.isKDEKeepAboveEnabled())

		kdeOptions = container.NewVBox(
			widget.NewLabel("KDE Window Settings"),
			keepAboveToggle,
		)
	}

	// Wayland-specific options
	var waylandOptions *fyne.Container
	if cm.isWayland {
		wlCopyCheck := widget.NewCheck("Use wl-clipboard utilities", func(checked bool) {
			// This is already handled in the code based on isWayland
			// This UI element is just for user information
		})
		wlCopyCheck.SetChecked(true) // Always checked when on Wayland
		wlCopyCheck.Disable()        // Prevent changing, it's always needed on Wayland

		waylandOptions = container.NewVBox(
			widget.NewLabel("Wayland Options"),
			wlCopyCheck,
			widget.NewLabel("Make sure wl-clipboard is installed:"),
			widget.NewButton("Install wl-clipboard", func() {
				// Show dialog with install command
				dialog.ShowInformation("Install wl-clipboard",
					"Run this command in terminal to install wl-clipboard:\n\nsudo pacman -S wl-clipboard",
					settingsWindow)
			}),
		)
	}

	// Settings layout
	hotkeyContainer := container.NewVBox(
		hotkeyLabel,
		currentHotkeyLabel,
		hotkeyDetector,
	)

	// Build settings content
	vbox := container.NewVBox(
		widget.NewLabel("Clipboard Settings"),
		autostartToggle,
		historyToggle,
		clearHistoryButton,
		widget.NewSeparator(),
		hotkeyContainer,
	)

	// Add KDE options if applicable
	if kdeOptions != nil {
		vbox.Add(widget.NewSeparator())
		vbox.Add(kdeOptions)
	}

	// Add Wayland options if applicable
	if waylandOptions != nil {
		vbox.Add(widget.NewSeparator())
		vbox.Add(waylandOptions)
	}

	// Add spacer and close button
	vbox.Add(layout.NewSpacer())
	vbox.Add(widget.NewButton("Close", func() {
		settingsWindow.Close()
	}))

	settingsWindow.SetContent(container.NewPadded(vbox))
	settingsWindow.Show()
}

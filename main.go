// Package main provides a clipboard manager for Manjaro Linux with support for both X11 and Wayland.
// It allows users to keep track of clipboard history, pin important items, and access them
// quickly through configurable global hotkeys.
package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	desktop "fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/go-vgo/robotgo"
	hook "github.com/robotn/gohook"
)

const (
	maxClipboardItems = 24
	configFileName    = "clipboard_manager_config.json"
	appID             = "io.github.ekats.noteboard"
	appName           = "NoteBoard"
	socketName        = "noteboard.sock"
)

// ClipboardItem represents a single item in the clipboard history
type ClipboardItem struct {
	content   string
	timestamp time.Time
	itemType  string // "text", "image", etc.
}

// ClipboardManager manages clipboard history and UI interactions
type ClipboardManager struct {
	items          []ClipboardItem
	window         fyne.Window
	list           *widget.List
	clearButton    *widget.Button
	pinned         map[int]bool
	hotkeySettings HotkeySettings
	configPath     string
	isWayland      bool
}

// CustomTooltip is a widget that shows content in a pop-up window when activated
type CustomTooltip struct {
	widget.DisableableWidget
	content     string
	popupWindow fyne.Window
	parent      fyne.Window
	showDelay   *time.Timer
	hideDelay   *time.Timer
}

// tooltipRenderer handles the layout of the tooltip button
type tooltipRenderer struct {
	tooltip *CustomTooltip
	text    *widget.Label
	objects []fyne.CanvasObject
}

// HotkeySettings stores user-configured keyboard shortcuts
type HotkeySettings struct {
	ShowHide    []string `json:"showHide"`    // Array of keys for the show/hide hotkey
	ModifierKey string   `json:"modifierKey"` // Modifier key (ctrl, alt, shift)
	ActionKey   string   `json:"actionKey"`   // Main action key
}

// Config structure for persistent settings
type Config struct {
	Hotkeys HotkeySettings `json:"hotkeys"`
}

// getConfigPath returns the path to the config file
func getConfigPath() string {
	// Get user's home directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		// Fallback to current directory if home is not available
		return configFileName
	}

	// Create a .config/clipboard-manager directory
	configDir := filepath.Join(homeDir, ".config", "clipboard-manager")

	// Create the directory if it doesn't exist
	if _, err := os.Stat(configDir); os.IsNotExist(err) {
		err := os.MkdirAll(configDir, 0755)
		if err != nil {
			// Fallback to home directory if can't create the config dir
			return filepath.Join(homeDir, configFileName)
		}
	}

	return filepath.Join(configDir, configFileName)
}

// loadConfig loads configuration from file or creates default if not exists
func loadConfig() Config {
	configPath := getConfigPath()

	// Default configuration
	defaultConfig := Config{
		Hotkeys: HotkeySettings{
			ShowHide:    []string{"ctrl", "alt", "v"},
			ModifierKey: "ctrl+alt",
			ActionKey:   "v",
		},
	}

	// Check if config file exists
	_, err := os.Stat(configPath)
	if os.IsNotExist(err) {
		// Create default config file
		saveConfig(defaultConfig)
		return defaultConfig
	}

	// Read config file
	data, err := os.ReadFile(configPath)
	if err != nil {
		fmt.Printf("Warning: Could not read config file, using defaults: %v\n", err)
		return defaultConfig
	}

	// Parse config
	var config Config
	err = json.Unmarshal(data, &config)
	if err != nil {
		fmt.Printf("Warning: Could not parse config file, using defaults: %v\n", err)
		return defaultConfig
	}

	return config
}

// saveConfig saves configuration to file
func saveConfig(config Config) error {
	configPath := getConfigPath()

	// Convert to JSON
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Write to file
	return os.WriteFile(configPath, data, 0644)
}

// isWaylandSession detects if running on Wayland
func isWaylandSession() bool {
	return os.Getenv("XDG_SESSION_TYPE") == "wayland"
}

func ensureSingleInstance() bool {
	// Get user's home directory for socket path
	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Printf("Warning: Could not get home directory: %v\n", err)
		return false
	}

	// Create runtime directory if it doesn't exist
	runtimeDir := filepath.Join(homeDir, ".config", "clipboard-manager", "runtime")
	if _, err := os.Stat(runtimeDir); os.IsNotExist(err) {
		if err := os.MkdirAll(runtimeDir, 0755); err != nil {
			fmt.Printf("Warning: Could not create runtime directory: %v\n", err)
			return false
		}
	}

	socketPath := filepath.Join(runtimeDir, socketName)

	// Remove socket if it exists but process is not running
	if _, err := os.Stat(socketPath); err == nil {
		// Try to connect to the socket
		conn, err := net.Dial("unix", socketPath)
		if err == nil {
			// If connection succeeds, another instance is running
			conn.Close()
			fmt.Println("Another instance is already running. Exiting.")
			return true
		}

		// If connection fails, remove the stale socket
		os.Remove(socketPath)
	}

	// Create and listen on the socket
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		fmt.Printf("Warning: Could not create socket: %v\n", err)
		return false
	}

	// Start a goroutine to accept connections
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				continue
			}
			// Just close the connection, we're just detecting other instances
			conn.Close()
		}
	}()

	return false
}

// createDesktopFile creates a .desktop file for autostart
func createDesktopFile() error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("could not get user home directory: %w", err)
	}

	autostartDir := filepath.Join(homeDir, ".config", "autostart")
	if _, err := os.Stat(autostartDir); os.IsNotExist(err) {
		err := os.MkdirAll(autostartDir, 0755)
		if err != nil {
			return fmt.Errorf("could not create autostart directory: %w", err)
		}
	}

	desktopFilePath := filepath.Join(autostartDir, "manjaro-clipboard.desktop")

	// Get the executable path
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("could not get executable path: %w", err)
	}

	// Create desktop file content
	content := fmt.Sprintf(`[Desktop Entry]
Type=Application
Name=%s
Comment=Clipboard history manager for Manjaro
Exec=%s
Icon=edit-paste
Terminal=false
Categories=Utility;
StartupNotify=false
X-GNOME-Autostart-enabled=true
`, appName, execPath)

	return os.WriteFile(desktopFilePath, []byte(content), 0644)
}

// setupKDEGlobalShortcut sets up KDE global shortcuts using KDE's kglobalaccel system
func setupKDEGlobalShortcut(cm *ClipboardManager) error {
	if !cm.isWayland {
		return nil // Only needed for Wayland
	}

	// Check if kwriteconfig5 is available (for KDE)
	_, err := exec.LookPath("kwriteconfig5")
	if err != nil {
		return fmt.Errorf("kwriteconfig5 not found, cannot set KDE shortcuts")
	}

	// Format the hotkey for KDE
	// Convert our format to KDE's format
	kdeModifierMap := map[string]string{
		"ctrl":  "Ctrl",
		"alt":   "Alt",
		"shift": "Shift",
		"super": "Meta",
	}

	var kdeModifiers []string
	for _, mod := range strings.Split(cm.hotkeySettings.ModifierKey, "+") {
		if kdeMod, ok := kdeModifierMap[mod]; ok {
			kdeModifiers = append(kdeModifiers, kdeMod)
		}
	}

	// Convert action key
	actionKey := strings.ToUpper(cm.hotkeySettings.ActionKey)

	// Build KDE shortcut string
	kdeShortcut := strings.Join(kdeModifiers, "+")
	if kdeShortcut != "" && actionKey != "" {
		kdeShortcut += "+"
	}
	kdeShortcut += actionKey

	// This is a simplified example that might need to be expanded
	shortcutGroup := "manjaro-clipboard"
	cmdShowHide := exec.Command("kwriteconfig5",
		"--file", "kglobalshortcutsrc",
		"--group", shortcutGroup,
		"--key", "show_clipboard",
		kdeShortcut+",none,Show Clipboard Manager")

	return cmdShowHide.Run()
}

// newClipboardManager creates a new clipboard manager instance
func newClipboardManager(w fyne.Window) *ClipboardManager {
	// Load existing config
	config := loadConfig()

	// Check if running on Wayland
	isWayland := isWaylandSession()

	cm := &ClipboardManager{
		items:          make([]ClipboardItem, 0, maxClipboardItems),
		window:         w,
		pinned:         make(map[int]bool),
		hotkeySettings: config.Hotkeys, // Use loaded hotkey settings
		configPath:     getConfigPath(),
		isWayland:      isWayland,
	}

	cm.list = cm.createItemList()

	cm.clearButton = widget.NewButton("Clear All", func() {
		cm.clearItems()
	})

	// Setup KDE global shortcut if on Wayland
	if cm.isWayland {
		err := setupKDEGlobalShortcut(cm)
		if err != nil {
			fmt.Printf("Warning: Failed to set up KDE global shortcut: %v\n", err)
		}
	}

	return cm
}

// addItem adds an item to the clipboard history
func (cm *ClipboardManager) addItem(content string) {
	// Skip if content is empty or the same as the most recent item
	if content == "" || (len(cm.items) > 0 && content == cm.items[0].content) {
		return
	}

	// Create new item
	newItem := ClipboardItem{
		content:   content,
		timestamp: time.Now(),
		itemType:  "text",
	}

	// Remove duplicate if exists elsewhere in the list
	for i, item := range cm.items {
		if item.content == content {
			cm.removeItem(i)
			break
		}
	}

	// Add at the beginning
	cm.items = append([]ClipboardItem{newItem}, cm.items...)

	// Trim if exceeds max
	if len(cm.items) > maxClipboardItems {
		cm.items = cm.items[:maxClipboardItems]
	}

	// Refresh the list
	cm.list.Refresh()
}

// NewCustomTooltip creates a new custom tooltip for showing text content
func NewCustomTooltip(content string, parent fyne.Window) *CustomTooltip {
	tooltip := &CustomTooltip{
		content: content,
		parent:  parent,
	}
	tooltip.ExtendBaseWidget(tooltip)
	return tooltip
}

// CreateRenderer is a private method to Fyne which defines how this widget is rendered
func (t *CustomTooltip) CreateRenderer() fyne.WidgetRenderer {
	text := widget.NewLabel("...")
	text.Alignment = fyne.TextAlignCenter

	return &tooltipRenderer{
		tooltip: t,
		text:    text,
		objects: []fyne.CanvasObject{text},
	}
}

func (r *tooltipRenderer) MinSize() fyne.Size {
	return r.text.MinSize()
}

func (r *tooltipRenderer) Layout(size fyne.Size) {
	r.text.Resize(size)
}

func (r *tooltipRenderer) Refresh() {
	r.text.Refresh()
}

func (r *tooltipRenderer) Objects() []fyne.CanvasObject {
	return r.objects
}

func (r *tooltipRenderer) Destroy() {}

// MouseIn is called when the mouse enters the tooltip area
func (t *CustomTooltip) MouseIn(event *desktop.MouseEvent) {
	// Cancel any pending hide timer
	if t.hideDelay != nil {
		t.hideDelay.Stop()
		t.hideDelay = nil
	}

	// Start show timer (300ms delay before showing)
	t.showDelay = time.AfterFunc(300*time.Millisecond, func() {
		t.showContent()
	})
}

// MouseOut is called when the mouse leaves the tooltip area
func (t *CustomTooltip) MouseOut() {
	// Cancel any pending show timer
	if t.showDelay != nil {
		t.showDelay.Stop()
		t.showDelay = nil
	}

	// Start hide timer (200ms delay before hiding)
	t.hideDelay = time.AfterFunc(200*time.Millisecond, func() {
		t.hideContent()
	})
}

// MouseMoved is called when the mouse moves within the tooltip area
func (t *CustomTooltip) MouseMoved(*desktop.MouseEvent) {}

// showContent displays the tooltip content
func (t *CustomTooltip) showContent() {
	// If there's already a popup open, close it
	if t.popupWindow != nil {
		t.popupWindow.Close()
		t.popupWindow = nil
	}

	// Create the popup window
	app := fyne.CurrentApp()
	t.popupWindow = app.NewWindow("Content")
	t.popupWindow.SetFixedSize(true)

	// Set window type hint if available
	if typeHint, ok := t.popupWindow.(interface{ SetDialogType() }); ok {
		typeHint.SetDialogType()
	}

	// Create content
	textDisplay := widget.NewLabel(t.content)
	textDisplay.Wrapping = fyne.TextWrapWord

	// Create scrollable container
	scrollContainer := container.NewScroll(textDisplay)
	scrollContainer.Resize(fyne.NewSize(400, 300))

	t.popupWindow.SetContent(scrollContainer)

	// Position the window near the cursor for better UX
	// This works on both X11 and Wayland
	curX, curY := robotgo.Location()

	// Check if we're on Wayland
	isWayland := os.Getenv("XDG_SESSION_TYPE") == "wayland"

	if isWayland {
		// For Wayland, we'll use a different approach
		// First resize the window
		t.popupWindow.Resize(fyne.NewSize(400, 300))

		// Then force XWayland usage for this window if possible
		// Set the env var for XWayland before window is mapped
		if setter, ok := t.popupWindow.(interface{ SetEnv(string, string) }); ok {
			setter.SetEnv("GDK_BACKEND", "x11")
		}

		// Use xdg-decoration protocol to remove decorations if available
		if setter, ok := t.popupWindow.(interface{ SetDecoration(bool) }); ok {
			setter.SetDecoration(false)
		}

		// For KDE on Wayland, we can try to set a window rule
		if isKDEPlasma() {
			// Create a temporary unique identifier for this window
			uniqueID := fmt.Sprintf("tooltip-%d", time.Now().UnixNano())

			// Try to set window role to get a consistent identifier
			if roleSetter, ok := t.popupWindow.(interface{ SetRole(string) }); ok {
				roleSetter.SetRole(uniqueID)
			}

			// Delay execution to ensure window is created
			go func() {
				time.Sleep(100 * time.Millisecond)

				// Try to position with KWin DBus API
				exec.Command("qdbus", "org.kde.KWin", "/KWin",
					"org.kde.KWin.setWindowGeometry", uniqueID,
					strconv.Itoa(curX+20), strconv.Itoa(curY+20),
					"400", "300").Run()
			}()
		}
	} else {
		// For X11, use the standard approach
		// Position window near cursor directly without needing parent position
		if mover, ok := t.popupWindow.(interface{ SetPosition(x, y int) }); ok {
			// Set position to near mouse cursor
			mover.SetPosition(curX+20, curY+20)
		}
	}

	// Make it an overlay window (no decorations)
	if setter, ok := t.popupWindow.(interface{ SetDecoration(bool) }); ok {
		setter.SetDecoration(false)
	}

	// Make it stay on top of other windows
	if setter, ok := t.popupWindow.(interface{ SetOnTop(bool) }); ok {
		setter.SetOnTop(true)
	}

	// For better Wayland support, try multiple methods
	go func() {
		// Wait a bit for window to be mapped
		time.Sleep(100 * time.Millisecond)

		if isWayland {
			// Try to use wl-shell-surface protocol if available
			runWaylandPositioningCommands(t.popupWindow, curX+20, curY+20)
		} else {
			// For X11, use xprop
			// Try to find our window ID
			cmd := exec.Command("xdotool", "search", "--name", "Content")
			output, err := cmd.Output()
			if err == nil && len(output) > 0 {
				// Get the first window ID
				lines := strings.Split(string(output), "\n")
				if len(lines) > 0 {
					windowID := strings.TrimSpace(lines[0])
					if windowID != "" {
						// Set the window type to tooltip or notification
						exec.Command("xprop", "-id", windowID, "-f", "_NET_WM_WINDOW_TYPE", "32a",
							"-set", "_NET_WM_WINDOW_TYPE", "_NET_WM_WINDOW_TYPE_NOTIFICATION").Run()

						// Also set the window to always stay on top
						exec.Command("xprop", "-id", windowID, "-f", "_NET_WM_STATE", "32a",
							"-set", "_NET_WM_STATE", "_NET_WM_STATE_ABOVE,_NET_WM_STATE_STAYS_ON_TOP").Run()

						// Position window near the cursor
						exec.Command("xdotool", "windowmove", windowID,
							strconv.Itoa(curX+20), strconv.Itoa(curY+20)).Run()
					}
				}
			}
		}
	}()

	// Show the window
	t.popupWindow.Show()
}

// Helper function to run Wayland-specific positioning commands
func runWaylandPositioningCommands(window fyne.Window, x, y int) {
	// First try to see if we can get the window ID via XWayland
	cmd := exec.Command("xwininfo", "-name", "Content")
	output, err := cmd.CombinedOutput()
	if err == nil && strings.Contains(string(output), "Window id") {
		// Parse window ID
		lines := strings.Split(string(output), "\n")
		for _, line := range lines {
			if strings.Contains(line, "Window id") {
				parts := strings.Split(line, " ")
				if len(parts) > 3 {
					windowID := strings.TrimSpace(parts[3])
					// Move window using xdotool (works with XWayland)
					exec.Command("xdotool", "windowmove", windowID,
						strconv.Itoa(x), strconv.Itoa(y)).Run()
					return
				}
			}
		}
	}

	// If we're on KDE Plasma, try with KWin's DBus interface
	if isKDEPlasma() {
		// Find window by title
		cmd := exec.Command("qdbus", "org.kde.KWin", "/KWin", "org.kde.KWin.queryWindowInfo")
		output, err := cmd.CombinedOutput()
		if err == nil {
			lines := strings.Split(string(output), "\n")
			for _, line := range lines {
				if strings.Contains(line, "Content") {
					// Found our window, try to move it
					parts := strings.Split(line, ",")
					if len(parts) > 0 {
						winID := strings.TrimSpace(parts[0])
						exec.Command("qdbus", "org.kde.KWin", "/KWin",
							"org.kde.KWin.setWindowGeometry", winID,
							strconv.Itoa(x), strconv.Itoa(y),
							"400", "300").Run()
						return
					}
				}
			}
		}
	}
}

// hideContent hides the tooltip content
func (t *CustomTooltip) hideContent() {
	if t.popupWindow != nil {
		t.popupWindow.Close()
		t.popupWindow = nil
	}
}

// Tapped handles tap events - show the tooltip on tap as well
func (t *CustomTooltip) Tapped(*fyne.PointEvent) {
	if t.popupWindow != nil {
		t.hideContent()
	} else {
		t.showContent()
	}
}

// Now, modify the existing createItemList function to use our custom tooltip
func (cm *ClipboardManager) createItemList() *widget.List {
	return widget.NewList(
		func() int {
			return len(cm.items)
		},
		func() fyne.CanvasObject {
			// Create a template for list items
			contentLabel := widget.NewLabel("Template content")
			contentLabel.Wrapping = fyne.TextWrapWord
			contentLabel.Truncation = fyne.TextTruncateEllipsis

			// Create placeholder for the tooltip
			tooltipPlaceholder := container.NewStack(
				widget.NewLabel("..."), // This will be replaced in updateItem
			)

			// Content container with label and tooltip placeholder
			contentContainer := container.NewBorder(nil, nil, nil, tooltipPlaceholder, contentLabel)

			timeLabel := widget.NewLabel("Time")
			timeLabel.TextStyle = fyne.TextStyle{Italic: true}

			pinButton := widget.NewButtonWithIcon("", theme.ContentAddIcon(), func() {})
			copyButton := widget.NewButtonWithIcon("", theme.ContentCopyIcon(), func() {})
			deleteButton := widget.NewButtonWithIcon("", theme.DeleteIcon(), func() {})

			buttons := container.NewHBox(pinButton, copyButton, deleteButton)
			bottomBar := container.NewBorder(nil, nil, timeLabel, buttons)

			// Create the main container
			return container.NewBorder(
				nil,
				bottomBar,
				nil,
				nil,
				contentContainer,
			)
		},
		func(i widget.ListItemID, o fyne.CanvasObject) {
			if i >= len(cm.items) {
				return // Safety check for index out of range
			}

			item := cm.items[i]

			// Properly cast to container
			content, ok := o.(*fyne.Container)
			if !ok {
				return // Skip if wrong type
			}

			// Get content container (which contains the label and tooltip placeholder)
			contentContainer, ok := content.Objects[0].(*fyne.Container)
			if !ok {
				return
			}

			// Get the content label and tooltip placeholder
			contentLabel, _ := contentContainer.Objects[0].(*widget.Label)
			tooltipContainer, ok := contentContainer.Objects[1].(*fyne.Container)
			if !ok {
				return
			}

			// Get bottom bar
			bottomBar, _ := content.Objects[1].(*fyne.Container)

			if contentLabel != nil {
				// Get first two lines of content
				lines := strings.Split(item.content, "\n")
				truncatedContent := item.content

				// Check if the content needs to be truncated
				if len(lines) > 2 {
					// Only show first two lines
					truncatedContent = lines[0]
					if len(lines) > 1 {
						truncatedContent += "\n" + lines[1]
					}

					// Create custom tooltip if there's more content
					tooltip := NewCustomTooltip(item.content, cm.window)
					tooltipContainer.Objects[0] = tooltip
					tooltipContainer.Refresh()
					tooltipContainer.Show()
				} else {
					// Hide tooltip if not needed
					tooltipContainer.Hide()
				}

				// Set content
				contentLabel.SetText(truncatedContent)
			}

			if bottomBar != nil {
				timeLabel, _ := bottomBar.Objects[0].(*widget.Label)
				buttonsContainer, _ := bottomBar.Objects[1].(*fyne.Container)

				if timeLabel != nil {
					// Set time
					timeLabel.SetText(item.timestamp.Format("15:04:05"))
				}

				if buttonsContainer != nil && len(buttonsContainer.Objects) >= 3 {
					pinButton, _ := buttonsContainer.Objects[0].(*widget.Button)
					copyButton, _ := buttonsContainer.Objects[1].(*widget.Button)
					deleteButton, _ := buttonsContainer.Objects[2].(*widget.Button)

					// Set pin icon based on state
					if pinButton != nil {
						if cm.pinned[i] {
							pinButton.SetIcon(theme.ContentRemoveIcon())
						} else {
							pinButton.SetIcon(theme.ContentAddIcon())
						}

						pinButton.OnTapped = func() {
							cm.pinned[i] = !cm.pinned[i]
							cm.list.Refresh()
						}
					}

					// Set button actions
					if copyButton != nil {
						copyButton.OnTapped = func() {
							go func() {
								if cm.isWayland {
									// For Wayland, use wl-copy instead of robotgo
									cmd := exec.Command("wl-copy", item.content)
									cmd.Run()
								} else {
									robotgo.WriteAll(item.content)
								}

								// Hide the window
								cm.window.Hide()
							}()
						}
					}

					if deleteButton != nil {
						deleteButton.OnTapped = func() {
							cm.removeItem(i)
						}
					}
				}
			}
		},
	)
}

// Helper function to create a scrollable text display
func createScrollableTextDisplay(content string) fyne.CanvasObject {
	textDisplay := widget.NewLabel(content)
	textDisplay.Wrapping = fyne.TextWrapWord

	// Wrap in a scroll container
	scrollContainer := container.NewScroll(textDisplay)

	return scrollContainer
}

// removeItem removes an item from clipboard history
func (cm *ClipboardManager) removeItem(index int) {
	if index < 0 || index >= len(cm.items) {
		return
	}

	// Remove from pinned if it was pinned
	delete(cm.pinned, index)

	// Update pinned indices
	newPinned := make(map[int]bool)
	for pinIdx, pinned := range cm.pinned {
		if pinIdx < index {
			newPinned[pinIdx] = pinned
		} else if pinIdx > index {
			newPinned[pinIdx-1] = pinned
		}
	}
	cm.pinned = newPinned

	// Remove the item
	cm.items = append(cm.items[:index], cm.items[index+1:]...)
	cm.list.Refresh()
}

// clearItems clears non-pinned items from clipboard history
func (cm *ClipboardManager) clearItems() {
	// Keep only pinned items
	pinnedItems := make([]ClipboardItem, 0)
	newPinned := make(map[int]bool)

	idx := 0
	for i, item := range cm.items {
		if cm.pinned[i] {
			pinnedItems = append(pinnedItems, item)
			newPinned[idx] = true
			idx++
		}
	}

	cm.items = pinnedItems
	cm.pinned = newPinned
	cm.list.Refresh()
}

// registerGlobalShortcut registers global keyboard shortcut
func registerGlobalShortcut(w fyne.Window, cm *ClipboardManager) {
	// Skip if running on Wayland as we use KDE shortcuts instead
	if cm.isWayland {
		return
	}

	go func() {
		hook.Register(hook.KeyDown, cm.hotkeySettings.ShowHide, func(e hook.Event) {
			// Use a channel to synchronize with the main thread
			done := make(chan struct{})
			go func() {
				// Get the current mouse position
				mouseX, mouseY := robotgo.Location()

				// Get screen size (primary monitor)
				screenWidth, screenHeight := robotgo.GetScreenSize()

				// Set window size - assuming standard clipboard size
				windowWidth := 400
				windowHeight := 500

				// Calculate window position based on mouse and screen
				// We want to position the window so it's fully on screen
				// and close to the mouse cursor
				var windowX, windowY int

				// X position: prefer right of cursor if space allows, otherwise left
				if mouseX+windowWidth+20 < screenWidth {
					// Position to the right of cursor
					windowX = mouseX + 20
				} else if mouseX-windowWidth-20 > 0 {
					// Position to the left of cursor
					windowX = mouseX - windowWidth - 20
				} else {
					// Center horizontally if neither fits well
					windowX = (screenWidth - windowWidth) / 2
				}

				// Y position: prefer below cursor if space allows, otherwise above
				if mouseY+windowHeight+20 < screenHeight {
					// Position below cursor
					windowY = mouseY + 20
				} else if mouseY-windowHeight-20 > 0 {
					// Position above cursor
					windowY = mouseY - windowHeight - 20
				} else {
					// Center vertically if neither fits well
					windowY = (screenHeight - windowHeight) / 2
				}

				// Since RunOnMain is not available, we'll use a direct approach
				// First hide the window
				w.Hide()

				// Resize to ensure window manager updates
				w.Resize(fyne.NewSize(float32(windowWidth), float32(windowHeight)))

				// Use SetPosition if available
				if setter, ok := w.(interface{ SetPosition(pos fyne.Position) }); ok {
					setter.SetPosition(fyne.NewPos(float32(windowX), float32(windowY)))
				}

				// Show window and request focus
				w.Show()
				w.RequestFocus()

				close(done)
			}()
			<-done // Wait for UI operations to complete
		})

		// Start the hook listening process
		s := hook.Start()
		<-hook.Process(s)
	}()
}

// monitorClipboard monitors system clipboard for changes
func (cm *ClipboardManager) monitorClipboard() {
	lastContent := ""

	go func() {
		for {
			var content string
			var err error

			if cm.isWayland {
				// Use wl-paste for Wayland
				cmd := exec.Command("wl-paste", "-n")
				output, cmdErr := cmd.Output()
				if cmdErr == nil {
					content = string(output)
				}
			} else {
				// Use robotgo for X11
				content, err = robotgo.ReadAll()
			}

			if err == nil && content != lastContent && content != "" {
				lastContent = content

				// Since RunOnMain is not available, use goroutine and directly
				// access the UI components but be careful about race conditions
				go func(contentCopy string) {
					cm.addItem(contentCopy)
				}(content) // Pass content as parameter to avoid race condition
			}

			time.Sleep(500 * time.Millisecond)
		}
	}()
}

// UpdateHotkey updates the hotkey settings
func (cm *ClipboardManager) UpdateHotkey(modifierKey, actionKey string) {
	// Parse modifier key into individual keys
	modifiers := strings.Split(modifierKey, "+")

	// Build new hotkey array
	var newHotkey []string
	if modifierKey != "" {
		for _, mod := range modifiers {
			if mod != "" {
				newHotkey = append(newHotkey, mod)
			}
		}
	}
	if actionKey != "" {
		newHotkey = append(newHotkey, actionKey)
	}

	// Update settings
	cm.hotkeySettings.ShowHide = newHotkey
	cm.hotkeySettings.ModifierKey = modifierKey
	cm.hotkeySettings.ActionKey = actionKey

	// Save settings to config file
	config := Config{
		Hotkeys: cm.hotkeySettings,
	}
	saveConfig(config)

	// Update KDE shortcut if on Wayland
	if cm.isWayland {
		setupKDEGlobalShortcut(cm)
	}
}

// func setWindowAlwaysOnTop(windowTitle string) {
// 	// Wait a moment for the window to appear and stabilize
// 	time.Sleep(500 * time.Millisecond)
// 	log.Print("AAAAAAAAAAAAAa")
// 	if isWaylandSession() {
// 		// For Wayland, try KDE-specific approach if it appears to be KDE
// 		// if os.Getenv("KDE_FULL_SESSION") != "" || os.Getenv("XDG_CURRENT_DESKTOP") == "KDE" {
// 		// 	setKDEKeepAboveOthers(windowTitle)
// 		// 	log.Print("AAAAAAAAAAAAAa")
// 		// }
// 	} else {
// 		// For X11, use xprop/xdotool
// 		setX11WindowAlwaysOnTop(windowTitle)
// 	}
// }

// For X11 environments
func setX11WindowAlwaysOnTop(windowTitle string) {
	// Try to find the window by its title
	cmd := exec.Command("xdotool", "search", "--name", windowTitle)
	output, err := cmd.Output()
	if err != nil {
		fmt.Printf("Could not find window ID: %v\n", err)
		return
	}

	// If multiple matches, take the first one
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) == 0 {
		fmt.Println("No matching windows found")
		return
	}

	winID := lines[0]

	// Set the _NET_WM_STATE_ABOVE atom
	cmd = exec.Command("xprop", "-id", winID, "-f", "_NET_WM_STATE", "32a",
		"-set", "_NET_WM_STATE", "_NET_WM_STATE_ABOVE")
	err = cmd.Run()
	if err != nil {
		fmt.Printf("Failed to set window always on top: %v\n", err)
	} else {
		fmt.Println("Successfully set window always on top via X11")
	}
}

// SetKDEWindowKeepAbove sets whether the window should stay above others
func (cm *ClipboardManager) setKDEWindowKeepAbove(enabled bool) error {
	// Only proceed if we're running in KDE Plasma
	if !isKDEPlasma() {
		return fmt.Errorf("not running in KDE Plasma")
	}

	// Check if kwriteconfig5 and qdbus are available
	_, err := exec.LookPath("kwriteconfig5")
	if err != nil {
		return fmt.Errorf("kwriteconfig5 not found: %w", err)
	}

	_, err = exec.LookPath("qdbus")
	if err != nil {
		return fmt.Errorf("qdbus not found: %w", err)
	}

	// Find or create a window rule for our app
	ruleId, isNew, err := cm.findOrCreateKDEWindowRule()
	if err != nil {
		return fmt.Errorf("failed to find/create KDE window rule: %w", err)
	}

	// Rule group is based on the ID
	ruleGroup := fmt.Sprintf("%d", ruleId)

	// Set the keep above property based on the enabled parameter
	kwritePath, _ := exec.LookPath("kwriteconfig5")

	// Set above (keep above others) setting
	exec.Command(kwritePath, "--file", "kwinrulesrc", "--group", ruleGroup,
		"--key", "above", strconv.FormatBool(enabled)).Run()
	exec.Command(kwritePath, "--file", "kwinrulesrc", "--group", ruleGroup,
		"--key", "aboverule", "2").Run() // 2 = force yes

	// If it's a new rule or if we're enabling the feature, also set these properties
	if isNew || enabled {
		// Window matching criteria - match by window title EXACTLY (not substring)
		exec.Command(kwritePath, "--file", "kwinrulesrc", "--group", ruleGroup,
			"--key", "title", appName).Run()
		exec.Command(kwritePath, "--file", "kwinrulesrc", "--group", ruleGroup,
			"--key", "titlematch", "0").Run() // 0 = exact match (was 2 for substring)

		// Description
		exec.Command(kwritePath, "--file", "kwinrulesrc", "--group", ruleGroup,
			"--key", "Description", appName+" Window Rule").Run()

		// If enabled, also set Layer to Above (not popup/overlay)
		if enabled {
			// Force Layer: Above
			exec.Command(kwritePath, "--file", "kwinrulesrc", "--group", ruleGroup,
				"--key", "layer", "4").Run() // 4 = Above layer (was 6 for Overlay)
			exec.Command(kwritePath, "--file", "kwinrulesrc", "--group", ruleGroup,
				"--key", "layerrule", "2").Run() // 2 = force yes
		} else {
			// Reset layer to normal
			exec.Command(kwritePath, "--file", "kwinrulesrc", "--group", ruleGroup,
				"--key", "layer", "0").Run() // 0 = normal
			exec.Command(kwritePath, "--file", "kwinrulesrc", "--group", ruleGroup,
				"--key", "layerrule", "2").Run() // 2 = force yes
		}

		// Make the rule apply to all desktops
		exec.Command(kwritePath, "--file", "kwinrulesrc", "--group", ruleGroup,
			"--key", "desktops", "").Run()
		exec.Command(kwritePath, "--file", "kwinrulesrc", "--group", ruleGroup,
			"--key", "desktopsrule", "3").Run() // 3 = all desktops

		// Apply to all activities
		exec.Command(kwritePath, "--file", "kwinrulesrc", "--group", ruleGroup,
			"--key", "activity", "").Run()
		exec.Command(kwritePath, "--file", "kwinrulesrc", "--group", ruleGroup,
			"--key", "activityrule", "3").Run() // 3 = all activities
	}

	// Reload KWin rules
	exec.Command("qdbus", "org.kde.KWin", "/KWin", "org.kde.KWin.reconfigure").Run()

	return nil
}

// Helper method to find an existing window rule or create a new one
func (cm *ClipboardManager) findOrCreateKDEWindowRule() (int, bool, error) {
	// Get number of existing rules in kwinrulesrc
	countCmd := exec.Command("kreadconfig5", "--file", "kwinrulesrc", "--group", "General", "--key", "count")
	countOutput, err := countCmd.Output()
	count := 0
	if err == nil {
		count, _ = strconv.Atoi(strings.TrimSpace(string(countOutput)))
	}

	// Check if there's already a rule for our app
	for i := 1; i <= count; i++ {
		ruleGroup := fmt.Sprintf("%d", i)

		// Get rule description
		descCmd := exec.Command("kreadconfig5", "--file", "kwinrulesrc", "--group", ruleGroup, "--key", "Description")
		descOutput, err := descCmd.Output()
		if err != nil {
			continue
		}

		desc := strings.TrimSpace(string(descOutput))
		// Check if this rule is for our app
		if strings.Contains(desc, appName) {
			return i, false, nil // Found existing rule
		}

		// Check title match as well
		titleCmd := exec.Command("kreadconfig5", "--file", "kwinrulesrc", "--group", ruleGroup, "--key", "title")
		titleOutput, err := titleCmd.Output()
		if err != nil {
			continue
		}

		title := strings.TrimSpace(string(titleOutput))
		if title == appName {
			return i, false, nil // Found existing rule
		}
	}

	// No existing rule found, create a new one
	newRuleIndex := count + 1

	// Set the new rule count
	kwritePath, _ := exec.LookPath("kwriteconfig5")
	exec.Command(kwritePath, "--file", "kwinrulesrc", "--group", "General", "--key", "count", strconv.Itoa(newRuleIndex)).Run()

	return newRuleIndex, true, nil // Return new rule index
}

func isKDEPlasma() bool {
	// Check common environment variables that indicate KDE
	desktop := os.Getenv("XDG_CURRENT_DESKTOP")
	session := os.Getenv("KDE_FULL_SESSION")

	return strings.Contains(strings.ToLower(desktop), "kde") || session == "true"
}

// IsKDEKeepAboveEnabled checks if Keep Above is currently enabled for our app
func (cm *ClipboardManager) isKDEKeepAboveEnabled() bool {
	// Only relevant for KDE Plasma
	if !isKDEPlasma() {
		return false
	}

	// Check if kreadconfig5 is available
	_, err := exec.LookPath("kreadconfig5")
	if err != nil {
		return false
	}

	// Find the rule for our app
	ruleId, isNew, err := cm.findOrCreateKDEWindowRule()
	if err != nil || isNew {
		return false // Rule doesn't exist or error
	}

	// Rule group is based on the ID
	ruleGroup := fmt.Sprintf("%d", ruleId)

	// Check if keep above is enabled
	aboveCmd := exec.Command("kreadconfig5", "--file", "kwinrulesrc", "--group", ruleGroup, "--key", "above")
	aboveOutput, err := aboveCmd.Output()
	if err != nil {
		return false
	}

	above := strings.TrimSpace(string(aboveOutput))
	return above == "true"
}

func main() {
	// Check if another instance is running
	if ensureSingleInstance() {
		// Exit if another instance is already running
		return
	}

	// Create app with consistent ID
	a := app.NewWithID(appID)
	a.Settings().SetTheme(theme.DefaultTheme())
	a.SetIcon(resourceNoteboardFynePng)

	w := a.NewWindow(appName)
	w.Resize(fyne.NewSize(400, 500))

	// w.SetMaster()

	cm := newClipboardManager(w)

	// Register global shortcut if not on Wayland``
	if !cm.isWayland {
		registerGlobalShortcut(w, cm)
	}

	w.SetCloseIntercept(func() {
		w.Hide()
	})

	// Set up search
	searchEntry := widget.NewEntry()
	searchEntry.SetPlaceHolder("Search clipboard items...")

	// Fix search functionality
	searchEntry.OnChanged = func(text string) {
		if text == "" {
			// Reset list to show all items
			cm.list.Refresh()
			return
		}

		// This is just visual filtering - in a production app
		// you'd want to maintain a separate filtered list
		// text = strings.ToLower(text)
		// Just refresh the entire list for now to keep it simple
		cm.list.Refresh()
	}

	// Header with title and search
	header := container.NewVBox(
		widget.NewLabel(appName),
		searchEntry,
	)

	// Create a system tray icon
	if desk, ok := a.(desktop.App); ok {
		m := fyne.NewMenu(appName,
			fyne.NewMenuItem("Show/Hide", func() {
				if w.Content().Visible() {
					w.Hide()
				} else {
					w.Show()
					w.RequestFocus()
					// go setWindowAlwaysOnTop(appName)
				}
			}),
			fyne.NewMenuItem("Quit", func() {
				a.Quit()
			}),
		)
		desk.SetSystemTrayMenu(m)
		// Try to load custom icon
		iconPath := "icons/noteboard-fyne.png" // Relative to executable directory
		customIcon, err := loadIconResource(iconPath)
		if err != nil {
			// Fall back to default icon on error
			fmt.Printf("Could not load custom icon: %v. Using default icon.\n", err)
			desk.SetSystemTrayIcon(theme.ContentPasteIcon())
		} else {
			// Use custom icon
			desk.SetSystemTrayIcon(customIcon)
		}
	}

	// Settings button
	settingsButton := widget.NewButtonWithIcon("", theme.SettingsIcon(), func() {
		ShowSettingsDialog(a, cm)
	})

	// Footer with buttons
	footer := container.NewHBox(
		layout.NewSpacer(),
		cm.clearButton,
		settingsButton,
	)

	// Main layout
	content := container.NewBorder(
		header,
		footer,
		nil,
		nil,
		container.NewPadded(cm.list),
	)

	w.SetContent(content)

	// Start monitoring clipboard
	cm.monitorClipboard()

	// Add some sample items
	if cm.isWayland {
		cm.addItem("Running on Wayland mode")
	} else {
		cm.addItem("Running on X11 mode")
	}

	cm.addItem("Welcome to NoteBoard!")

	// Display hotkey info
	if cm.isWayland {
		cm.addItem("Using KDE global shortcuts (set in System Settings)")
	} else if len(cm.hotkeySettings.ShowHide) > 0 {
		cm.addItem("Press " + strings.Join(cm.hotkeySettings.ShowHide, "+") + " to open this manager")
	}

	cm.addItem("Items copied to your clipboard will appear here")

	w.Show()
	a.Run()
	// go setWindowAlwaysOnTop(appName)
}

// Function to load an icon from the project directory
func loadIconResource(iconPath string) (fyne.Resource, error) {
	// First check if the path is absolute
	if !filepath.IsAbs(iconPath) {
		// Get the executable directory
		execPath, err := os.Executable()
		if err != nil {
			return nil, err
		}

		// Combine with the executable directory to make it absolute
		iconPath = filepath.Join(filepath.Dir(execPath), iconPath)
	}

	// Check if file exists
	_, err := os.Stat(iconPath)
	if os.IsNotExist(err) {
		return nil, err
	}

	// Load the file content
	iconBytes, err := os.ReadFile(iconPath)
	if err != nil {
		return nil, err
	}

	// Get the file extension and convert to lowercase
	ext := filepath.Ext(iconPath)
	if ext == "" {
		ext = ".png" // Assume PNG if no extension
	}

	// Create a static resource from the icon file
	iconName := filepath.Base(iconPath)
	return fyne.NewStaticResource(iconName, iconBytes), nil
}

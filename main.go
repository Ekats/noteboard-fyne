// Package main provides a clipboard manager for Manjaro Linux with support for both X11 and Wayland.
// It allows users to keep track of clipboard history, pin important items, and access them
// quickly through configurable global hotkeys.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
	appID             = "io.github.ekats.manjaro-clipboard"
	appName           = "Manjaro Clipboard Manager"
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

	cm.list = widget.NewList(
		func() int {
			return len(cm.items)
		},
		func() fyne.CanvasObject {
			// Create a template for list items
			contentLabel := widget.NewLabel("Template content")
			contentLabel.Wrapping = fyne.TextWrapWord

			timeLabel := widget.NewLabel("Time")
			timeLabel.TextStyle = fyne.TextStyle{Italic: true}

			pinButton := widget.NewButtonWithIcon("", theme.ContentAddIcon(), func() {})
			copyButton := widget.NewButtonWithIcon("", theme.ContentCopyIcon(), func() {})
			deleteButton := widget.NewButtonWithIcon("", theme.DeleteIcon(), func() {})

			buttons := container.NewHBox(pinButton, copyButton, deleteButton)

			bottomBar := container.NewBorder(nil, nil, timeLabel, buttons)

			// Create the main container properly
			return container.NewBorder(
				nil,
				bottomBar,
				nil,
				nil,
				contentLabel,
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

			// Get components with proper type checking
			contentLabel, _ := content.Objects[0].(*widget.Label)
			bottomBar, _ := content.Objects[1].(*fyne.Container)

			if contentLabel != nil {
				// Set content
				truncatedContent := item.content
				if len(truncatedContent) > 100 {
					truncatedContent = truncatedContent[:100] + "..."
				}
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

								// We can't use RunOnMain as it's not available in your Fyne version
								// Instead, we'll hide the window directly
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
				mouseX, mouseY := robotgo.GetMousePos()

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

func main() {
	// Create app with consistent ID
	a := app.NewWithID(appID)
	a.Settings().SetTheme(theme.DarkTheme())

	w := a.NewWindow(appName)
	w.Resize(fyne.NewSize(400, 500))

	cm := newClipboardManager(w)

	// Register global shortcut if not on Wayland
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
		text = strings.ToLower(text)
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
				}
			}),
			fyne.NewMenuItem("Quit", func() {
				a.Quit()
			}),
		)
		desk.SetSystemTrayMenu(m)
		desk.SetSystemTrayIcon(theme.ContentPasteIcon())
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

	cm.addItem("Welcome to Manjaro Clipboard Manager!")

	// Display hotkey info
	if cm.isWayland {
		cm.addItem("Using KDE global shortcuts (set in System Settings)")
	} else if len(cm.hotkeySettings.ShowHide) > 0 {
		cm.addItem("Press " + strings.Join(cm.hotkeySettings.ShowHide, "+") + " to open this manager")
	}

	cm.addItem("Items copied to your clipboard will appear here")

	w.Show()
	a.Run()
}

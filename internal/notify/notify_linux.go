// Package notify provides desktop notification support.
package notify

import (
	"fmt"

	"github.com/godbus/dbus/v5"
)

// Send sends a desktop notification via D-Bus (org.freedesktop.Notifications).
func Send(title, message string) error {
	conn, err := dbus.SessionBus()
	if err != nil {
		return fmt.Errorf("connecting to session bus: %w", err)
	}
	defer conn.Close()

	obj := conn.Object("org.freedesktop.Notifications", "/org/freedesktop/Notifications")
	call := obj.Call("org.freedesktop.Notifications.Notify", 0,
		"hydra",          // app_name
		uint32(0),        // replaces_id
		"",               // app_icon
		title,            // summary
		message,          // body
		[]string{},       // actions
		map[string]dbus.Variant{}, // hints
		int32(-1),        // expire_timeout (-1 = default)
	)
	return call.Err
}

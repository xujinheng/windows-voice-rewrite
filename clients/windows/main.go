package main

func main() { runApp() }

// trayMenuRequested handles WM_RBUTTONUP and WM_CONTEXTMENU notifications.
func trayMenuRequested(event uint32) bool {
	return event == 0x0205 || event == 0x007b
}

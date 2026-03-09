//go:build windows

package cmd

// getTermWidthIoctl returns 0 on Windows; width detection falls back to $COLUMNS.
func getTermWidthIoctl() int {
	return 0
}

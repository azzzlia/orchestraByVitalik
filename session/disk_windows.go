//go:build windows

package session

// DiskFreeGB is not measured on Windows.
func DiskFreeGB(string) float64 { return -1 }

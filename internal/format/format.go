// Package format centralizes human-facing formatting on the Go side so units
// and rendering stay consistent across the API, audit detail, and nvrctl.
// Values are stored canonically (bytes, epoch seconds, °C, m/s) and formatted
// only at the display boundary.
package format

import "fmt"

// Bytes renders a byte count with a binary-prefixed unit.
func Bytes(n int64) string {
	if n <= 0 {
		return "0 B"
	}
	units := []string{"B", "KB", "MB", "GB", "TB", "PB"}
	v := float64(n)
	i := 0
	for v >= 1024 && i < len(units)-1 {
		v /= 1024
		i++
	}
	if v < 10 && i > 0 {
		return fmt.Sprintf("%.1f %s", v, units[i])
	}
	return fmt.Sprintf("%.0f %s", v, units[i])
}

// Package secret handles masking of sensitive values and platform keychains.
package secret

// Mask returns a display-safe secret, keeping a prefix and suffix for long keys,
// and strictly hiding short keys with dynamic safety thresholds.
func Mask(s string) string {
	if s == "" {
		return ""
	}
	r := []rune(s)
	if len(r) <= 10 {
		if len(r) <= 4 {
			return "••••"
		}
		return string(r[:2]) + "••••" + string(r[len(r)-2:])
	}
	prefixLen := 6
	if len(r) < 14 {
		prefixLen = 3
	}
	return string(r[:prefixLen]) + "••••" + string(r[len(r)-4:])
}

package screens

func moveCursor(current, length, delta int) int {
	if length <= 0 {
		return current
	}

	next := (current + delta) % length
	if next < 0 {
		next += length
	}
	return next
}

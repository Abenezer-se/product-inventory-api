package products

import (
	"strconv"
	"strings"
)

// decimalPlaces returns the number of digits after the decimal point in the
// shortest exact decimal representation of v (for example 10.5 gives 1).
func decimalPlaces(v float64) int {
	s := strconv.FormatFloat(v, 'f', -1, 64)
	i := strings.IndexByte(s, '.')
	if i < 0 {
		return 0
	}
	return len(s) - i - 1
}

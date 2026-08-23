// Package abn provides the canonical Australian Business Number checksum.
package abn

var weights = [...]int{10, 1, 3, 5, 7, 9, 11, 13, 15, 17, 19}

func Valid(value string) bool {
	if len(value) != len(weights) {
		return false
	}
	total := 0
	for index := range value {
		if value[index] < '0' || value[index] > '9' {
			return false
		}
		digit := int(value[index] - '0')
		if index == 0 {
			digit--
		}
		total += digit * weights[index]
	}
	return total%89 == 0
}

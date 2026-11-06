package applyprogram

import "fmt"

func AddValidationCount(left int, right int) (int, error) {
	if left < 0 || right < 0 || left > int(^uint(0)>>1)-right {
		return 0, fmt.Errorf("forward effect validation count overflows")
	}
	return left + right, nil
}

func MultiplyValidationCount(value int, multiplier int) (int, error) {
	if value < 0 || multiplier < 0 || value != 0 && multiplier > int(^uint(0)>>1)/value {
		return 0, fmt.Errorf("forward effect validation count overflows")
	}
	return value * multiplier, nil
}

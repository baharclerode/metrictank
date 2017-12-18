package expr

import "errors"

var ErrIntPositive = errors.New("integer must be positive")

// Validator is a function to validate an input
type Validator func(e *expr) error

func IntPositive(e *expr) error {
	if e.int < 1 {
		return ErrIntPositive
	}
	return nil
}

func IsAggFunc(e *expr) error {
	if getCrossSeriesAggFunc(e.str) == nil {
		return errors.New("Invalid aggregation func: " + e.str)
	}
	return nil
}

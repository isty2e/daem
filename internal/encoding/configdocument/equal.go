package configdocument

import "math"

func equalNative(left, right Value) bool {
	switch left := left.data.(type) {
	case nullNode:
		_, ok := right.data.(nullNode)
		return ok
	case boolNode:
		right, ok := right.data.(boolNode)
		return ok && left == right
	case stringNode:
		right, ok := right.data.(stringNode)
		return ok && left == right
	case jsonNumberNode:
		right, ok := right.data.(jsonNumberNode)
		return ok && left == right
	case integerNode:
		right, ok := right.data.(integerNode)
		return ok && left == right
	case floatNode:
		right, ok := right.data.(floatNode)
		return ok && (math.Float64bits(float64(left)) == math.Float64bits(float64(right)) || math.IsNaN(float64(left)) && math.IsNaN(float64(right)))
	case temporalNode:
		right, ok := right.data.(temporalNode)
		return ok && left == right
	case arrayNode:
		right, ok := right.data.(arrayNode)
		if !ok || len(left) != len(right) {
			return false
		}
		for index := range left {
			if !equalNative(left[index], right[index]) {
				return false
			}
		}
		return true
	case objectNode:
		right, ok := right.data.(objectNode)
		if !ok || len(left) != len(right) {
			return false
		}
		for index := range left {
			if left[index].key != right[index].key || !equalNative(left[index].value, right[index].value) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

package executioncontext

type GroupOccupancy uint8

const (
	GroupAbsent GroupOccupancy = iota
	GroupPresent
	GroupUnsignalable
)

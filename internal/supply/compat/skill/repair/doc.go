// Package repair owns deterministic skill compatibility repair planning and
// replayable repair operations.
//
// It may transform copied artifact contents and produce recipe facts. It must
// not fetch sources, edit manifests, build locks, mutate host outputs, decide
// apply plans, or render CLI guidance.
package repair

// Package skill owns canonical desired Skill values and selector-backed
// SkillSet generator semantics.
//
// It owns skill identity, source admissibility, target/scope/install policy,
// selectors, and inherited child construction. It does not list or fetch
// sources, resolve artifacts, choose host paths, repair content, or manage
// lifecycle state.
package skill

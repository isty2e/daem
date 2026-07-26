package manifest

const starterContent = "version = 1\ntargets = [\"codex\"]\n"

// StarterContent returns the canonical starter declaration used by init.
func StarterContent() []byte {
	return []byte(starterContent)
}

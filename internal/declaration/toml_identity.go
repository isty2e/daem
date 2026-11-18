package declaration

func (skill Skill) Key() Key {
	name := skill.ID
	if name == "" {
		name = skill.Name
	}
	return Key{Kind: KindSkill, Name: name}
}

func (skill Skill) TargetValues() Targets {
	return Targets(skill.Targets).Values()
}

func (group SkillGroup) MemberKeys() []Key {
	keys := make([]Key, 0, len(group.Names))
	for _, name := range group.Names {
		keys = append(keys, Key{Kind: KindSkill, Name: name})
	}
	return keys
}

func (group SkillGroup) TargetValues() Targets {
	return Targets(group.Targets).Values()
}

func (hook Hook) Key() Key {
	return Key{Kind: KindHook, Name: hook.Name}
}

func (hook Hook) TargetValues() Targets {
	return Targets(hook.Targets).Values()
}

func (instructions Instructions) TargetValues() Targets {
	return Targets(instructions.Targets).Values()
}

// Key returns the document-local identity of the host configuration declaration.
func (agentConfig AgentConfig) Key() Key {
	return Key{Kind: KindAgentConfig, Name: agentConfig.ID}
}

func (agentConfig AgentConfig) TargetValues() Targets {
	return Targets(agentConfig.Targets).Values()
}

package codec

import tomlsyntax "github.com/isty2e/daem/internal/encoding/toml"

func (source *SkillSource) UnmarshalTOML(content []byte) error {
	value, err := tomlsyntax.DecodeValue(content)
	if err != nil {
		return err
	}
	return source.decodeTOMLValue(value)
}

func (source *InstructionSource) UnmarshalTOML(content []byte) error {
	value, err := tomlsyntax.DecodeValue(content)
	if err != nil {
		return err
	}
	return source.decodeTOMLValue(value)
}

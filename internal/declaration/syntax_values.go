package declaration

import tomlsyntax "github.com/isty2e/daem/internal/encoding/toml"

func (source *Source) UnmarshalTOML(content []byte) error {
	value, err := tomlsyntax.DecodeValue(content)
	if err != nil {
		return err
	}
	return source.decodeTOMLValue(value)
}

func (source *HookAssetSource) UnmarshalTOML(content []byte) error {
	value, err := tomlsyntax.DecodeValue(content)
	if err != nil {
		return err
	}
	return source.decodeTOMLValue(value)
}

func (reference *MCPEnvReference) UnmarshalTOML(content []byte) error {
	value, err := tomlsyntax.DecodeValue(content)
	if err != nil {
		return err
	}
	return reference.decodeTOMLValue(value)
}

func (source *InstructionSource) UnmarshalTOML(content []byte) error {
	value, err := tomlsyntax.DecodeValue(content)
	if err != nil {
		return err
	}
	return source.decodeTOMLValue(value)
}

func (command *MCPCommand) UnmarshalTOML(content []byte) error {
	value, err := tomlsyntax.DecodeValue(content)
	if err != nil {
		return err
	}
	return command.decodeTOMLValue(value)
}

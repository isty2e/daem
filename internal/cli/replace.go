package cli

import "strings"

type replacementKeyValues []string

func (values *replacementKeyValues) String() string {
	return strings.Join(*values, ",")
}

func (values *replacementKeyValues) Set(raw string) error {
	*values = append(*values, raw)
	return nil
}

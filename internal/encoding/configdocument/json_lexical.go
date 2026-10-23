package configdocument

// The normalized parser input changes only line endings and supplies the EOF
// newline required by HuJSON. Stored source and lexical comment text stay raw.
type jsonLexical struct {
	comments   []string
	normalized []byte
}

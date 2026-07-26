package archguard

import (
	"os"
	"path/filepath"
)

func packageFileContent(record PackageRecord, fileName string) ([]byte, bool) {
	if record.FileContents != nil {
		content, ok := record.FileContents[fileName]
		if ok {
			return []byte(content), true
		}
	}
	if record.Dir == "" {
		return nil, false
	}
	content, err := os.ReadFile(filepath.Join(record.Dir, fileName))
	if err != nil {
		return nil, false
	}
	return content, true
}

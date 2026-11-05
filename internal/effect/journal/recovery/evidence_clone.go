package recovery

func ClonePathEvidence(values []PathEvidence) []PathEvidence {
	result := make([]PathEvidence, len(values))
	for index, value := range values {
		if value.PathMode != nil {
			mode := *value.PathMode
			value.PathMode = &mode
		}
		result[index] = value
	}
	return result
}

func CloneBackupEvidence(values []BackupEvidence) []BackupEvidence {
	result := make([]BackupEvidence, len(values))
	copy(result, values)
	return result
}

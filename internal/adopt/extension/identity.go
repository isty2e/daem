package extension

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	desiredextension "github.com/isty2e/daem/internal/desired/extension"
)

const (
	digestStageBase = iota
	digestStage12
	digestStage24
	digestStageFull
)

func assignExtensionIDs(
	candidates map[desiredextension.CarrierKey]candidateFact,
	existing []desiredextension.Extension,
) (map[desiredextension.CarrierKey]string, error) {
	return assignExtensionIDsWithDigest(candidates, existing, relationDigest)
}

func assignExtensionIDsWithDigest(
	candidates map[desiredextension.CarrierKey]candidateFact,
	existing []desiredextension.Extension,
	digestFor func(desiredextension.CarrierKey) string,
) (map[desiredextension.CarrierKey]string, error) {
	if digestFor == nil {
		return nil, fmt.Errorf("extension relation digest function is required")
	}
	fixedByID := make(map[string]desiredextension.CarrierKey, len(existing))
	fixedByRelation := make(map[desiredextension.CarrierKey]string, len(existing))
	for _, value := range existing {
		id := value.ID().Name()
		key := value.CarrierKey()
		if previous, duplicate := fixedByID[id]; duplicate && previous != key {
			return nil, fmt.Errorf(
				"existing extension id %q names different relations",
				id,
			)
		}
		if previous, duplicate := fixedByRelation[key]; duplicate && previous != id {
			return nil, fmt.Errorf(
				"existing extension relation %s appears under ids %q and %q",
				canonicalRelationText(key),
				previous,
				id,
			)
		}
		fixedByID[id] = key
		fixedByRelation[key] = id
	}

	assigned := make(map[desiredextension.CarrierKey]string, len(candidates))
	stages := make(map[desiredextension.CarrierKey]int, len(candidates))
	bases := make(map[desiredextension.CarrierKey]string, len(candidates))
	digests := make(map[desiredextension.CarrierKey]string, len(candidates))
	for key, candidate := range candidates {
		if fixed, exists := fixedByRelation[key]; exists {
			assigned[key] = fixed
			continue
		}
		base := generatedExtensionBase(candidate)
		bases[key] = base
		digest := digestFor(key)
		if len(digest) != sha256.Size*2 {
			return nil, fmt.Errorf(
				"extension relation digest for %s must contain %d hexadecimal characters",
				canonicalRelationText(key),
				sha256.Size*2,
			)
		}
		if _, err := hex.DecodeString(digest); err != nil {
			return nil, fmt.Errorf(
				"extension relation digest for %s is not hexadecimal",
				canonicalRelationText(key),
			)
		}
		digests[key] = digest
		assigned[key] = base
	}

	for {
		byID := make(map[string][]desiredextension.CarrierKey)
		for key, id := range assigned {
			if _, fixed := fixedByRelation[key]; fixed {
				continue
			}
			byID[id] = append(byID[id], key)
		}
		advance := make(map[desiredextension.CarrierKey]struct{})
		for id, keys := range byID {
			if len(keys) > 1 {
				for _, key := range keys {
					advance[key] = struct{}{}
				}
			}
			if _, collision := fixedByID[id]; collision {
				for _, key := range keys {
					advance[key] = struct{}{}
				}
			}
		}
		if len(advance) == 0 {
			return assigned, nil
		}
		for key := range advance {
			if stages[key] == digestStageFull {
				return nil, fmt.Errorf(
					"extension id collision remains at full digest for %s",
					canonicalRelationText(key),
				)
			}
			stages[key]++
			assigned[key] = extensionIDAtStage(
				bases[key],
				digests[key],
				stages[key],
			)
		}
	}
}

func generatedExtensionBase(candidate candidateFact) string {
	stem := sanitizeExtensionStem(string(candidate.loadIdentity))
	return stem + "-" +
		string(candidate.key.Target()) + "-" +
		string(candidate.key.Scope())
}

func sanitizeExtensionStem(value string) string {
	var builder strings.Builder
	pendingSeparator := false
	started := false
	for _, character := range value {
		asciiAlnum := character >= 'A' && character <= 'Z' ||
			character >= 'a' && character <= 'z' ||
			character >= '0' && character <= '9'
		allowed := asciiAlnum || character == '.' || character == '_' || character == '-'
		if !started {
			if !asciiAlnum {
				continue
			}
			started = true
		}
		if allowed {
			if pendingSeparator && builder.Len() != 0 {
				builder.WriteByte('-')
			}
			pendingSeparator = false
			builder.WriteRune(character)
			continue
		}
		pendingSeparator = true
	}
	stem := strings.TrimRight(builder.String(), ".-_")
	if stem == "" {
		return "extension"
	}
	return stem
}

func relationDigest(key desiredextension.CarrierKey) string {
	digest := sha256.Sum256(canonicalRelationBytes(key))
	return hex.EncodeToString(digest[:])
}

func extensionIDAtStage(base string, digest string, stage int) string {
	switch stage {
	case digestStageBase:
		return base
	case digestStage12:
		return base + "-" + digest[:12]
	case digestStage24:
		return base + "-" + digest[:24]
	case digestStageFull:
		return base + "-" + digest
	default:
		panic(fmt.Sprintf("unsupported extension id digest stage %d", stage))
	}
}

func canonicalRelationBytes(key desiredextension.CarrierKey) []byte {
	fields := []string{
		string(key.Carrier()),
		string(key.Target()),
		string(key.Scope()),
		string(key.Source().Kind()),
		key.Source().Ref(),
	}
	var builder strings.Builder
	for _, field := range fields {
		builder.WriteString(strconv.Itoa(len(field)))
		builder.WriteByte(':')
		builder.WriteString(field)
		builder.WriteByte('\n')
	}
	return []byte(builder.String())
}

func canonicalRelationText(key desiredextension.CarrierKey) string {
	return string(canonicalRelationBytes(key))
}

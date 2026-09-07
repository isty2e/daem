package extension

import desiredextension "github.com/isty2e/daem/internal/desired/extension"

const (
	claudePluginCarrierSubjectNamespace      = "claude-code.plugin-carrier"
	codexPluginCarrierSubjectNamespace       = "codex.plugin-carrier"
	openCodePluginCarrierSubjectNamespace    = "opencode.plugin-carrier"
	piPackageCarrierSubjectNamespace         = "pi.package-carrier"
	antigravityPluginCarrierSubjectNamespace = "antigravity-cli.plugin-carrier"
)

type carrierNamespaceRow struct {
	carrier   desiredextension.Carrier
	namespace string
}

var carrierNamespaceCatalog = [...]carrierNamespaceRow{
	{carrier: desiredextension.CarrierClaudeCodePlugin, namespace: claudePluginCarrierSubjectNamespace},
	{carrier: desiredextension.CarrierCodexPlugin, namespace: codexPluginCarrierSubjectNamespace},
	{carrier: desiredextension.CarrierOpenCodePlugin, namespace: openCodePluginCarrierSubjectNamespace},
	{carrier: desiredextension.CarrierPiPackage, namespace: piPackageCarrierSubjectNamespace},
	{carrier: desiredextension.CarrierAntigravityCLIPlugin, namespace: antigravityPluginCarrierSubjectNamespace},
}

// CarrierNamespace returns the topology subject namespace owned by one closed
// carrier. Forged carriers have no namespace.
func CarrierNamespace(carrier desiredextension.Carrier) (string, bool) {
	for _, row := range carrierNamespaceCatalog {
		if row.carrier == carrier {
			return row.namespace, true
		}
	}
	return "", false
}

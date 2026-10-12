package extension

import "testing"

func TestNPMRegistryTargetKeepsInstallationIdentitySeparate(t *testing.T) {
	for _, test := range []struct {
		operand, installed, registry string
		known                        bool
	}{
		{"tool", "tool", "tool", true},
		{"@team/tool@^2.0.0", "@team/tool", "@team/tool", true},
		{"shim@npm:tool@2.0.0", "shim", "tool", true},
		{"@team/shim@npm:@vendor/tool", "@team/shim", "@vendor/tool", true},
		{"shim@NPM:tool@latest", "shim", "tool", true},
		{"tool@npm:tool@2.0.0", "tool", "tool", true},
		{"shim@npm:tool@npm:other@1.0.0", "shim", "", false},
		{"tool@file:./checkout", "tool", "", false},
		{"tool@https://example.com/plugin.tgz", "tool", "", false},
		{"shim@npm:tool@token=fixture-value", "shim", "", false},
	} {
		t.Run(test.operand, func(t *testing.T) {
			spec, ok := ParseNPMPackageSpec(test.operand)
			if !ok {
				t.Fatal("fixture operand was not structurally parsed")
			}
			registry, known := spec.RegistryTargetName()
			if spec.Name() != test.installed || registry != test.registry || known != test.known {
				t.Fatalf("installed/registry identity = %q/%q (known=%t), want %q/%q (known=%t)", spec.Name(), registry, known, test.installed, test.registry, test.known)
			}
		})
	}
	if name, known := (NPMPackageSpec{}).RegistryTargetName(); name != "" || known {
		t.Fatal("zero npm operand supplied registry authority")
	}
}

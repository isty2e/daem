package ownership

import "fmt"

func addressFootprintLabel(address ManagedAddress) string {
	footprint := address.Footprint()
	if footprint.Validate() != nil {
		return "invalid footprint"
	}
	if key, selected := footprint.Key(); selected {
		return fmt.Sprintf("key %q", key.String())
	}
	return "whole path"
}

package clijson

type MCPStatusDimension struct {
	Dimension string `json:"dimension"`
	State     string `json:"state"`
	Reason    string `json:"reason"`
}

type MCPStatus struct {
	Subject struct {
		Kind      string `json:"kind"`
		Namespace string `json:"namespace"`
		Name      string `json:"name"`
	} `json:"subject"`
	Target                 string               `json:"target"`
	Scope                  string               `json:"scope"`
	ConfigPath             string               `json:"config_path"`
	ContentPath            string               `json:"content_path"`
	AdapterContractVersion string               `json:"adapter_contract_version"`
	Projection             []MCPStatusDimension `json:"projection_dimensions"`
	Host                   []MCPStatusDimension `json:"host_dimensions"`
	Delegate               []MCPStatusDimension `json:"delegate_dimensions"`
	Runtime                []MCPStatusDimension `json:"runtime_dimensions"`
	Residue                []MCPStatusDimension `json:"residue_dimensions"`
	Other                  []MCPStatusDimension `json:"other_dimensions"`
}

func (status MCPStatus) Dimensions() []MCPStatusDimension {
	dimensions := make([]MCPStatusDimension, 0)
	dimensions = append(dimensions, status.Projection...)
	dimensions = append(dimensions, status.Host...)
	dimensions = append(dimensions, status.Delegate...)
	dimensions = append(dimensions, status.Runtime...)
	dimensions = append(dimensions, status.Residue...)
	dimensions = append(dimensions, status.Other...)
	return dimensions
}

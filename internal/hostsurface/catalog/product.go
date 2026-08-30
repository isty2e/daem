package catalog

var productCatalog = mustProduct()

// Product returns the compiled MCP host-surface catalog for current owner
// catalogs. Owner catalogs remain the fact source; consumers select compiled
// cells, runtime-probe purpose, provider-authoring admission, subject lookup,
// and owner-order MCP enumeration from this snapshot.
func Product() Catalog {
	return productCatalog
}

func mustProduct() Catalog {
	catalog, err := Compile(productSeed())
	if err != nil {
		panic(err)
	}
	return catalog
}

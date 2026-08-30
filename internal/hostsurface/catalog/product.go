package catalog

var productCatalog = mustProduct()

// Product returns the compiled MCP host-surface catalog for current owner
// catalogs. Owner catalogs remain the fact source; consumers select compiled
// cells, runtime-probe purpose, and provider-authoring admission from this
// snapshot.
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

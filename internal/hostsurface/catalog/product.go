package catalog

var productCatalog = mustProduct()

// Product returns the compiled MCP host-surface catalog for current owner
// catalogs. Production selection still uses those owner catalogs directly.
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

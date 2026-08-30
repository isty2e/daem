package catalog

var productCatalog = mustProduct()

// Product returns the immutable compiled host-surface catalog for current owner
// catalogs. MCP and reviewed non-MCP views are active consumer inputs; each
// remaining consumer cuts over only after differential parity.
func Product() Catalog {
	return productCatalog
}

func mustProduct() Catalog {
	catalog, err := Compile(productSeed())
	if err != nil {
		panic(err)
	}
	catalog, err = catalog.withManagedPathSurfaces(productManagedPathSeed())
	if err != nil {
		panic(err)
	}
	catalog, err = catalog.withHookSurfaces(productHookSeed(), productHookAssetSeed())
	if err != nil {
		panic(err)
	}
	catalog, err = catalog.withExtensionSurfaces(productExtensionSeed())
	if err != nil {
		panic(err)
	}
	return catalog
}

package relationhost

func defaultObserverCatalog() ([]passiveObserver, error) {
	return newObserverCatalog([]passiveObserver{
		antigravityCLIPluginObserver(),
		claudePluginObserver(),
		codexPluginObserver(),
		openCodePluginObserver(),
		piPackageObserver(),
	})
}

# Daem architecture

The LikeC4 model shows the Go CLI, the owners of its decisions and data, and its effects on host processes and files. Open the views from the repository root:

```sh
likec4 start architecture
```

Start with **Daem in context**. **Semantic ownership and data flow** follows the facts from declaration to effect. **Host-Surface, Operation-Safety and the State Barrier** separates static host metadata, operation planning and effect authority. **Host and durable boundaries** shows how execution and recovery reach files. Open a model element's links to see the corresponding source.

**Direct workflow service imports** shows actual Go imports between the named packages. Other views show data flow or runtime interaction. An interaction can cross several packages; it is not an import claim. The [workflow import policy](../internal/archguard/workflow_imports.go) governs permissions, including permissions a caller may not use. The model does not draw a permitted but unused dependency as a live edge.

To check the model or build the views outside the checkout:

```sh
likec4 validate architecture
likec4 build architecture --output "$(mktemp -d)"
```

These checks exercise the LikeC4 sources and views. Compare changes to the model with the linked code and tests: a valid relationship or a rendered diagram does not establish that an import or call exists.

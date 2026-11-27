# Shared ownership

## Purpose

Define authority to manage an output when manifests share a physical destination or data namespace. [Output ownership](../../../internal/output/ownership) and [carrier claims](../../../internal/effect/storage/carrierclaim) implement separate registries. [Managed output](../managed-output/spec.md) owns the content comparison; [carrier lifecycle](../carrier-lifecycle/spec.md) describes relation settlement.

## Requirements

### Requirement: Prevent overlapping managed output claims

Daem SHALL permit one owner per whole output or overlapping configuration projection. Disjoint contributions in one file can have different owners when they can be edited losslessly. Compare the physical destination and projection, not just the target name or spelled path; an alias or an unselected target does not bypass the check. An equal payload is not co-ownership. A shared copied output can retain other consumers when just one selected consumer is removed; do not remove the physical output while another authorized consumer remains.

Report `ownership_conflict` with the observed owner when available. Claims for an interrupted acquisition or release stay reserved or active until the owning operation is settled; a missing journal does not turn a reservation into a vacant claim. A declaration removal normally releases authority only with the committed host and state change. Neither `--manage-existing` nor deleting an input declaration overrides these checks.

#### Scenario: Two manifests select disjoint MCP bindings in one settings file

- **WHEN** each has an admitted independent contribution at the same physical document
- **THEN** neither needs authority over the other's binding or over the entire document

#### Scenario: One manifest tries to adopt an output reserved by another

- **WHEN** the first manifest's interrupted operation still owns its reservation
- **THEN** an otherwise matching adoption cannot steal or treat that reservation as absent

### Requirement: Keep stored references, claims, and observations distinct

A recorded document binding SHALL identify an owning state authority, its logical destination and the physical namespace it observed. The binding does not reserve a path, grant mutation authority or prove the current namespace still matches. Output claims and bindings are independent of carrier claims. Multiple known manifests may consume a carrier; each relation keeps exact target, scope, source, route and owner evidence. Neither registry enumerates ambient consumers or supplies general package-store ownership.

#### Scenario: A binding survives a filesystem change

- **WHEN** the selected destination no longer identifies the bound namespace
- **THEN** the stored binding does not let daem act on the new file as if that namespace were unchanged

### Requirement: Reject reconstruction of a lost installed baseline

When the selected statefile is absent but settled output claims for it remain, a command needing the applied baseline SHALL refuse instead of initializing fresh ownership. Desired sources, lock contents, current host bytes, and the claimant's registry entry cannot reconstruct what was last applied. A binding or permission protection alone does not establish lost state; any independent document fence still applies. A retired state location is not an empty active baseline.

`recover --release-output-ownership` can explicitly release the exact selected absent statefile's settled output claims and document bindings, including claims from other manifests sharing that state selection. Revalidate absence, owner/location identity, authority fences, and the eligible claim set before writing. A restored statefile, unexplained ownership, reservation, pending recovery or unresolved relocation requirement refuses release. Release leaves installed outputs, other owners and the separate carrier registry intact. It does not clear any journal, file-set fence, or permission record. A usable matching backup instead permits recovery of the original baseline without releasing ownership.

#### Scenario: A backup is restored while release waits for confirmation

- **WHEN** the selected statefile is no longer absent at execution
- **THEN** release refuses rather than discarding the claims for a now-present baseline

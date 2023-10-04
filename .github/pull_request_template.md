**What changed?**

<!-- Describe your changes here- ideally you can get that description straight from your descriptive commit message(s)! -->

- [ ] Has [Docs](https://github.com/weaveworks/gitopssets-controller/tree/main/docs) if any changes are user facing, including updates to minimum requirements e.g. Kubernetes version bumps.
- [ ] Has Tests included if any functionality added or changed.
- [ ] Has an [Example](https://github.com/weaveworks/gitopssets-controller/tree/main/examples) if the change requires configuration to use.
- [ ] Release notes block below has been updated with any user facing changes (API changes, bug fixes, changes requiring upgrade notices or deprecation warnings).
- [ ] Release notes contains the string "action required" if the change requires additional action from users switching to the new release.

For new Generators the above notes apply, with the following additional items:

- [ ] The Generator has been added to the Matrix generator's `GitOpsSetNestedGenerator`, this should be done by default unless there's some reason it doesn't work.
- [ ] The Generator has been added to the list of configurable [Generators](https://github.com/weaveworks/gitopssets-controller/blob/main/pkg/setup/generators.go), if you do not, the generator **cannot** be enabled!
- [ ] If the Generator depends on Kubernetes resources, a Watch has been added to track changes to the resources in the controller.

# Release Notes

```release-note
NONE
```

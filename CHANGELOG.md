# Changelog

## [0.17.3](https://github.com/weaveworks/gitopssets-controller/compare/v0.17.2...v0.17.3) (2026-10-06)


### Bug Fixes

* adopt unowned resources that already exist ([6377ae0](https://github.com/weaveworks/gitopssets-controller/commit/6377ae0dc3aa6fe1cf2764610d45051067334219)), closes [#274](https://github.com/weaveworks/gitopssets-controller/issues/274)
* classify namespaced resources with the REST mapper ([2ae7c9d](https://github.com/weaveworks/gitopssets-controller/commit/2ae7c9d14326b68530d0d31a68f42913bb9331a4)), closes [#268](https://github.com/weaveworks/gitopssets-controller/issues/268)
* do not panic when repeat matches a scalar ([5b395b2](https://github.com/weaveworks/gitopssets-controller/commit/5b395b275b54c657a094dd8d4afe2ee0db1443a6)), closes [#265](https://github.com/weaveworks/gitopssets-controller/issues/265)
* drop null creationTimestamp from rendered objects ([9d6196e](https://github.com/weaveworks/gitopssets-controller/commit/9d6196e613a3dd84575ead1b858a832d32aac650))
* fail the API client when TLS setup fails ([977ce81](https://github.com/weaveworks/gitopssets-controller/commit/977ce8110b26e023ec6a23cbe30cb4f74858baf0)), closes [#269](https://github.com/weaveworks/gitopssets-controller/issues/269)
* follow every page of pull requests ([53e7f4a](https://github.com/weaveworks/gitopssets-controller/commit/53e7f4a348ed7582e2d210e4b49eb8c8281ea535)), closes [#272](https://github.com/weaveworks/gitopssets-controller/issues/272)
* garbage collect suspended GitOpsSets on delete ([c17e8f4](https://github.com/weaveworks/gitopssets-controller/commit/c17e8f4bf52cd6119e4afb4c8ee8b3bd06c111d1)), closes [#273](https://github.com/weaveworks/gitopssets-controller/issues/273)
* give the e2e REST mapper the envtest credentials ([8287a21](https://github.com/weaveworks/gitopssets-controller/commit/8287a213d286615fa97d8b6852854b15c5cd34a4))
* index GitOpsSets that use the cluster generator ([180c932](https://github.com/weaveworks/gitopssets-controller/commit/180c932624be27e0f894cb7898346b192923a821)), closes [#277](https://github.com/weaveworks/gitopssets-controller/issues/277)
* keep resources in inventory until delete succeeds ([8dc7ac6](https://github.com/weaveworks/gitopssets-controller/commit/8dc7ac6a40812b9216ce463e7fa254fd5979cdbf)), closes [#266](https://github.com/weaveworks/gitopssets-controller/issues/266)
* match no clusters when the selector is empty ([2ed092a](https://github.com/weaveworks/gitopssets-controller/commit/2ed092adc4bf1f768b1c06f9726bf1a4c98577bf)), closes [#271](https://github.com/weaveworks/gitopssets-controller/issues/271)
* read generator inputs with the impersonated client ([a55e81e](https://github.com/weaveworks/gitopssets-controller/commit/a55e81e388976b53c325115e58ce12278597f9c3)), closes [#270](https://github.com/weaveworks/gitopssets-controller/issues/270)
* remove non-deterministic Sprig functions from templates ([4100ef6](https://github.com/weaveworks/gitopssets-controller/commit/4100ef6d6eea2dc6dc18c01394c5163f376a0a4d)), closes [#276](https://github.com/weaveworks/gitopssets-controller/issues/276)
* return toYaml marshal errors from templates ([fe06ee6](https://github.com/weaveworks/gitopssets-controller/commit/fe06ee65da5da3bd093107b1cb1352b8bd699b05)), closes [#275](https://github.com/weaveworks/gitopssets-controller/issues/275)
* treat an empty matrix axis as an empty product ([4d17e93](https://github.com/weaveworks/gitopssets-controller/commit/4d17e9330305ddc541d4532bec8b9c21dff0a9b9)), closes [#267](https://github.com/weaveworks/gitopssets-controller/issues/267)

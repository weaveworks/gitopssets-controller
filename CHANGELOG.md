# Changelog

## Unreleased

* The default install no longer runs kube-rbac-proxy. Metrics stay on 127.0.0.1:8080. An overlay that deletes containers/0, including gcp-infrastructure `platform/gitopssets`, must drop that patch before upgrading or it deletes the manager. (#300)

## [0.19.0](https://github.com/weaveworks/gitopssets-controller/compare/v0.18.0...v0.19.0) (2026-10-06)


### Features

* publish gitopssets-cli binaries with the GitHub release ([1b642e4](https://github.com/weaveworks/gitopssets-controller/commit/1b642e43ec370f70bf8b1ca241ea0d6141ae9bf1))
* publish the CLI image and multi-arch controller images ([db0b93f](https://github.com/weaveworks/gitopssets-controller/commit/db0b93fdd1ca4295f7807d55cfc6505ec7d88681))
* skip unchanged applies and let templates wait, orphan, and impersonate ([769a8fc](https://github.com/weaveworks/gitopssets-controller/commit/769a8fc1e30d23a43a3a189ab3cf5c3b0d7a7b6e)), closes [#295](https://github.com/weaveworks/gitopssets-controller/issues/295) [#296](https://github.com/weaveworks/gitopssets-controller/issues/296) [#297](https://github.com/weaveworks/gitopssets-controller/issues/297) [#298](https://github.com/weaveworks/gitopssets-controller/issues/298) [#299](https://github.com/weaveworks/gitopssets-controller/issues/299) [#300](https://github.com/weaveworks/gitopssets-controller/issues/300)
* validate specs, expose file paths, and limit rollout batches ([aaa8fc8](https://github.com/weaveworks/gitopssets-controller/commit/aaa8fc83cce95ad1c987ed038a165bd53c5fc021)), closes [#14](https://github.com/weaveworks/gitopssets-controller/issues/14) [#159](https://github.com/weaveworks/gitopssets-controller/issues/159) [#80](https://github.com/weaveworks/gitopssets-controller/issues/80)

## [0.18.0](https://github.com/weaveworks/gitopssets-controller/compare/v0.17.3...v0.18.0) (2026-10-06)


### Features

* add a deletion policy that can orphan rendered resources ([7b9d3d7](https://github.com/weaveworks/gitopssets-controller/commit/7b9d3d7ed7079b9cae9d4df870e4ea6050d75898)), closes [#287](https://github.com/weaveworks/gitopssets-controller/issues/287)
* add pull request branch, author, and label fields ([6096d16](https://github.com/weaveworks/gitopssets-controller/commit/6096d16e34f36c6882faf85ef1be07a0fbb26c09)), closes [#286](https://github.com/weaveworks/gitopssets-controller/issues/286)
* apply namespaces before the objects that use them ([0d85bca](https://github.com/weaveworks/gitopssets-controller/commit/0d85bca905433558060db96648508c52844fd43e)), closes [#282](https://github.com/weaveworks/gitopssets-controller/issues/282)
* apply rendered resources with server-side apply ([256f2eb](https://github.com/weaveworks/gitopssets-controller/commit/256f2ebaba6981a818125d481121d87484d473f6)), closes [#284](https://github.com/weaveworks/gitopssets-controller/issues/284)
* expose cluster secret, CAPI, and connectivity fields ([a911f0d](https://github.com/weaveworks/gitopssets-controller/commit/a911f0db36a54f31490ec692603f3b954acfaed8)), closes [#279](https://github.com/weaveworks/gitopssets-controller/issues/279)
* filter generated elements with CEL ([8a105f1](https://github.com/weaveworks/gitopssets-controller/commit/8a105f1d8f2731020cf429424b4ffbbeb378d6bb)), closes [#289](https://github.com/weaveworks/gitopssets-controller/issues/289)
* record per-object apply time and the last error ([163cd02](https://github.com/weaveworks/gitopssets-controller/commit/163cd02d76c8346588d4e11f2db5b40bb167fd48)), closes [#283](https://github.com/weaveworks/gitopssets-controller/issues/283)
* refuse link-local and cluster addresses in the API client ([0c9dcfd](https://github.com/weaveworks/gitopssets-controller/commit/0c9dcfde7e4b750308be990c18d9a43f08892156)), closes [#288](https://github.com/weaveworks/gitopssets-controller/issues/288)
* report resource health separately from Ready ([dc5178b](https://github.com/weaveworks/gitopssets-controller/commit/dc5178b68bc02d4fabcf596484ab9614ed66d28e)), closes [#280](https://github.com/weaveworks/gitopssets-controller/issues/280)
* skip rendering when source digests are unchanged ([44e8943](https://github.com/weaveworks/gitopssets-controller/commit/44e8943de25118b28cb9b601d0bde86a53283a56)), closes [#290](https://github.com/weaveworks/gitopssets-controller/issues/290)


### Bug Fixes

* include the reconcile error in Kubernetes events ([beaddcc](https://github.com/weaveworks/gitopssets-controller/commit/beaddcc17e983472d7013badab1333fd3926026e)), closes [#281](https://github.com/weaveworks/gitopssets-controller/issues/281)
* read OCI artifact digests from the watched API version ([a1b5f5a](https://github.com/weaveworks/gitopssets-controller/commit/a1b5f5a4b6628d2dc35245897ebdafb69ca82023))
* watch pull request and API client secrets ([8064d0b](https://github.com/weaveworks/gitopssets-controller/commit/8064d0b681f8a1b2545aa4d204241ede8f360b77)), closes [#285](https://github.com/weaveworks/gitopssets-controller/issues/285)

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

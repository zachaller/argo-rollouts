---
name: codegen
description: How to regenerate all generated code in argo-rollouts after editing API types, proto files, or mocked interfaces — deepcopy, clientsets, protobuf, openapi, CRDs, manifests, mocks, and docs. MANDATORY after any change to pkg/apis/rollouts/v1alpha1/types.go. Use when adding/changing API fields, changing rollout.proto, or when CI codegen-diff checks fail.
---

# Code Generation

CI verifies that generated code matches its sources; a `types.go` change without
regenerated outputs will fail the build. Everything is driven from the Makefile.

## The full pipeline

```bash
make install-tools-local   # one-time: codegen CLIs + protoc into ./dist (PATH-prepended)
make codegen               # gen-proto gen-k8scodegen gen-openapi gen-mocks gen-crd manifests docs
```

`make codegen` is slow. Run only the targets your change requires:

| You changed | Run | Regenerates |
|---|---|---|
| `pkg/apis/rollouts/v1alpha1/types.go` | `make gen-k8scodegen` | deepcopy + `pkg/client/` (clientset, informers, listers) via `hack/update-codegen.sh` |
| same | `make k8s-proto api-proto` | `generated.proto`, `generated.pb.go`, apiclient pb/gateway/swagger |
| same | `make gen-openapi` | `openapi_generated.go` + violation exceptions list |
| same | `make gen-crd` | `manifests/crds/*.yaml` (via `hack/gen-crd-spec`) |
| same | `make manifests` | `manifests/install.yaml`, `namespace-install.yaml` |
| `pkg/apiclient/rollout/rollout.proto` | `make api-proto` then `pnpm --dir ui run protogen` | Go pb + gateway + swagger, then UI TS models |
| A mocked interface (`metric.Provider`, `TrafficRoutingReconciler`, step plugin ifaces) | `make gen-mocks` | `*/mocks/` (interfaces listed in `hack/update-mocks.sh`) |
| CLI commands / notification docs | `make docs` | generated docs under `docs/` |

Practical order for a types.go change:
`gen-k8scodegen → k8s-proto → api-proto → gen-openapi → gen-crd → manifests`.

## Rules for editing types.go

- Additive changes only; never reuse or renumber `protobuf=` field numbers in tags.
- Every field needs json + protobuf tags and doc comments (they become CRD/openapi docs);
  optional fields need `+optional` and a pointer or omitempty as appropriate.
- Add validation in `pkg/apis/rollouts/validation/` and defaulting in `utils/defaults`.
- New openapi violations may need entries in `pkg/apis/api-rules/violation_exceptions.list`
  (gen-openapi maintains it).

## Gotchas

- Tools install into `./dist` and the Makefile prepends it to PATH — don't install
  global versions to "fix" missing tools; run `make install-go-tools-local` /
  `install-protoc-local`.
- Codegen runs `go mod vendor` (`go-mod-vendor` target); a dirty `vendor/` diff
  afterwards is expected if deps changed, otherwise revert stray vendor noise.
- proto generation briefly creates `github.com/` and `k8s.io/` dirs in the repo root;
  the Makefile cleans them up — if a run aborted, `make clean` or delete them manually.
- Commit generated changes together with the source change, and never hand-edit
  generated files (list in the `refactoring` skill).

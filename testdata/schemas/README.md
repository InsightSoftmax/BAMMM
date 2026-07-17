# Vendored CRD JSON schemas

These JSON Schemas back the Tier 2 schema validation run by
`scripts/validate-schemas.sh` (and the `Schema validation` CI job). kubeconform
uses them to validate BAMMM's emitted CRD manifests; built-in resources
(batch/v1 Job, v1 ConfigMap) come from kubeconform's default schema location.

Layout matches the kubeconform `-schema-location` template
`{{.Group}}/{{.ResourceKind}}_{{.ResourceAPIVersion}}.json`:

| File | Source |
|---|---|
| `kueue.x-k8s.io/localqueue_v1beta1.json` | [datreeio/CRDs-catalog](https://github.com/datreeio/CRDs-catalog) |
| `batch.volcano.sh/job_v1alpha1.json` | Converted from the upstream Volcano CRD (below) |
| `jobset.x-k8s.io/jobset_v1alpha2.json` | Converted from the upstream JobSet CRD (below) |

## Regenerating the converted schemas

The Volcano and JobSet CRDs are not in the CRDs-catalog, so they are converted
from upstream with kubeconform's `openapi2jsonschema.py`:

```sh
curl -sSL https://raw.githubusercontent.com/yannh/kubeconform/master/scripts/openapi2jsonschema.py -o /tmp/openapi2jsonschema.py

# Volcano Job
curl -sSL https://raw.githubusercontent.com/volcano-sh/volcano/master/config/crd/volcano/bases/batch.volcano.sh_jobs.yaml -o /tmp/volcano-jobs-crd.yaml
( cd /tmp && uv run --with pyyaml python openapi2jsonschema.py volcano-jobs-crd.yaml )
mv /tmp/job_v1alpha1.json testdata/schemas/batch.volcano.sh/job_v1alpha1.json

# JobSet
curl -sSL https://raw.githubusercontent.com/kubernetes-sigs/jobset/main/config/components/crd/bases/jobset.x-k8s.io_jobsets.yaml -o /tmp/jobset-crd.yaml
( cd /tmp && uv run --with pyyaml python openapi2jsonschema.py jobset-crd.yaml )
mv /tmp/jobset_v1alpha2.json testdata/schemas/jobset.x-k8s.io/jobset_v1alpha2.json
```

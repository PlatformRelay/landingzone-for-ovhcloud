# object-storage-user

One Public Cloud user for S3 access, whose policy allows only the buckets it is given (FR-004, FR-013; research R5
*Per-tenant buckets*): the `objectstore_operator` role and no other, one S3 credential, and an S3
policy. The user has no name or tags of its own; its description comes from
[`modules/naming`](../naming/README.md) (kind `s3_user`).

```hcl
module "state_user" {
  source      = "../../modules/object-storage-user"
  project_id  = var.state_project_id
  description = module.state_user_name.name
  buckets     = [module.state_bucket.name]
}
```

## Policy

- **Allow** object and listing actions (`s3:GetObject`, `s3:PutObject`, `s3:DeleteObject`,
  `s3:ListBucket`, `s3:GetBucketLocation`, the multipart actions, and the version reads
  `s3:ListBucketVersions`, `s3:GetObjectVersion`) on `arn:aws:s3:::<bucket>` and
  `arn:aws:s3:::<bucket>/*` of each given bucket, nothing else. No bucket configuration action and no
  `s3:DeleteObjectVersion` is allowed by the policy; whether the ACL fallback below still lets the
  credential suspend versioning or purge old state versions is UNVERIFIED. Form and action set as in the read-write example of docs.ovhcloud.com
  `storage-and-backup/object-storage/s3-identity-and-access-management` (*Read/write access to a
  bucket and its objects*).
- **Deny** `s3:ListAllMyBuckets` on `*`: every S3 user may list all buckets of the account by default
  (same guide, *Deny listing of all buckets owned by the parent account*).
- **Deny** the actions in `deny_actions` on `*` (statement `DenyGivenActions`, unconditioned), when
  the caller names any; none by default. `components/state-backend` names the history and
  bucket-configuration writes its state bucket must keep (spec 005 T016).
- What an action neither allowed nor denied does falls back to the bucket ACLs (same guide,
  *permissions are evaluated*); what `objectstore_operator` grants there is UNVERIFIED offline (T010).

## Inputs

| Name | Type | Default | Meaning |
|---|---|---|---|
| `project_id` | string | required | Public Cloud project id (service name) that holds the user and the buckets. |
| `description` | string | required | User description from `modules/naming` (kind `s3_user`). |
| `buckets` | list(string) | required | Bucket names the policy allows; at least one, each within the bucket-name charset and length (no wildcard, ARN or path; `a..b` is not refused here). |
| `deny_actions` | list(string) | `[]` | S3 actions denied on `*` in one unconditioned statement; each `s3:<Action>`, no wildcard. |

## Outputs

| Name | Meaning |
|---|---|
| `user_id` | Id of the user. |
| `access_key_id` | Access key id of the S3 credential. |
| `secret_access_key` | Secret access key; sensitive. Never published (G2). |
| `policy` | The S3 policy document (JSON) the policy resource carries. |

## Resources

Provider docs (ovh 2.21.0; attributes used exist in the pinned schema, `task lint` validates):
- `ovh_cloud_project_user` — [`cloud_project_user`](https://registry.terraform.io/providers/ovh/ovh/2.21.0/docs/resources/cloud_project_user) (`service_name`, `description`, `role_names`)
- `ovh_cloud_project_user_s3_credential` — [`cloud_project_user_s3_credential`](https://registry.terraform.io/providers/ovh/ovh/2.21.0/docs/resources/cloud_project_user_s3_credential) (`service_name`, `user_id`, `access_key_id`, `secret_access_key`)
- `ovh_cloud_project_user_s3_policy` — [`cloud_project_user_s3_policy`](https://registry.terraform.io/providers/ovh/ovh/2.21.0/docs/resources/cloud_project_user_s3_policy) (`service_name`, `user_id`, `policy`)

Tests: `tests/unit.tftest.hcl`; `task test:unit -- modules/object-storage-user`. The committed
`.terraform.lock.hcl` is the test lock file (ADR-0011, amendment 2026-10-07).

# One S3 user confined to the given buckets (FR-004, FR-013; research R5 *Per-tenant buckets*): the
# `objectstore_operator` role, one S3 credential, and a policy that allows object and listing
# actions on the given buckets only (form of docs.ovhcloud.com storage-and-backup/object-storage/
# s3-identity-and-access-management.mdx:116-125). Every S3 user may list all buckets of the account
# by default; the policy denies it explicitly (same guide, :145 and :150-158).
resource "ovh_cloud_project_user" "this" {
  service_name = var.project_id
  description  = var.description
  role_names   = ["objectstore_operator"]
}

resource "ovh_cloud_project_user_s3_credential" "this" {
  service_name = var.project_id
  user_id      = ovh_cloud_project_user.this.id
}

resource "ovh_cloud_project_user_s3_policy" "this" {
  service_name = var.project_id
  user_id      = ovh_cloud_project_user.this.id
  policy = jsonencode({
    Statement = [
      {
        Sid    = "GivenBuckets"
        Effect = "Allow"
        Action = [
          "s3:GetObject",
          "s3:PutObject",
          "s3:DeleteObject",
          "s3:ListBucket",
          "s3:GetBucketLocation",
          "s3:ListBucketVersions",
          "s3:GetObjectVersion",
          "s3:ListMultipartUploadParts",
          "s3:ListBucketMultipartUploads",
          "s3:AbortMultipartUpload",
        ]
        Resource = flatten([for b in var.buckets : ["arn:aws:s3:::${b}", "arn:aws:s3:::${b}/*"]])
      },
      {
        Sid      = "DenyListAllBuckets"
        Effect   = "Deny"
        Action   = ["s3:ListAllMyBuckets"]
        Resource = ["*"]
      },
    ]
  })
}

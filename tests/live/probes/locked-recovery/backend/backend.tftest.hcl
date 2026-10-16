# Offline controls of the disposable backend root (spec 003 T036): plan only, no backend, no
# credential, no provider. What lz-live's check relies on: a plan always has one change (so writer
# A reaches its approval prompt), and the root has nothing a destroy or a state would need.

run "one_change_to_approve" {
  command = plan

  assert {
    condition     = terraform_data.writer.input == "lzprobe-locked-recovery"
    error_message = "the backend root plans exactly its one terraform_data"
  }
}

# The S3 backend block is read from the file (a test runs without a backend; each condition also
# names the resource, as a test assert must): the lockfile is on,
# the key is the one the check's lock object derives from, and no bucket is named here (init names
# the run's).
run "backend_uses_the_lockfile_on_a_fixed_key" {
  command = plan

  assert {
    condition = alltrue([
      terraform_data.writer.input != "",
      length(regexall("(?m)^  backend \"s3\" \\{$", file("main.tf"))) == 1,
      length(regexall("(?m)^    use_lockfile += true$", file("main.tf"))) == 1,
      length(regexall("(?m)^    key += \"lzprobe-locked-recovery/terraform.tfstate\"$", file("main.tf"))) == 1,
      length(regexall("(?m)^    (bucket|access_key|secret_key|profile|shared_credentials_files|dynamodb_table) ", file("main.tf"))) == 0,
    ])
    error_message = "backend s3 with use_lockfile = true and key lzprobe-locked-recovery/terraform.tfstate, no bucket, no credential, no DynamoDB table"
  }

  assert {
    condition     = terraform_data.writer.input != "" && length(regexall("(?m)^(resource|data|module) ", file("main.tf"))) == 1 && length(regexall("(?m)^resource \"terraform_data\" \"writer\" \\{$", file("main.tf"))) == 1
    error_message = "the backend root holds one terraform_data and nothing else"
  }
}

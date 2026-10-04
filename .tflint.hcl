# TFLint configuration for the offline static checks (ADR-0008 L0).
# Only the terraform ruleset bundled with the pinned TFLint is used, so no
# plugin is downloaded. Module calls are not followed: each module directory
# is linted on its own, and the dependency checker owns cross-module rules.
config {
  call_module_type = "none"
}

plugin "terraform" {
  enabled = true
  preset  = "recommended"
}

# Generated interface documentation (ADR-0013) needs every input and output
# described; the recommended preset does not require it.
rule "terraform_documented_variables" {
  enabled = true
}

rule "terraform_documented_outputs" {
  enabled = true
}

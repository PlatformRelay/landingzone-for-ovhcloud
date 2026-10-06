# P6 (005 T007) control for p6-import-mock: the same import blocks under `tofu test` without a
# mock provider (terraform_data is built in, nothing leaves the sandbox).
run "plan_imports" {
  command = plan
}

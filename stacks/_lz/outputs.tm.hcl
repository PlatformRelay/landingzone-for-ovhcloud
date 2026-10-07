// Root outputs of every stack (005 T038; FR-005, research R4, coordinator decision 3 of T037):
// each output the stage declares, re-exported under its own name with its own sensitivity, so
// `tofu output -json` of the generated root is what the envelope builder reads.
generate_hcl "_lz_outputs.tf" {
  content {
    tm_dynamic "output" {
      for_each = global.lz.stage_outputs[global.lz.stage]
      labels   = [output.key]
      content {
        value     = tm_hcl_expression("module.${global.lz.module}.${output.key}")
        sensitive = output.value
      }
    }
  }
}

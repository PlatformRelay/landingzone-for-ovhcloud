run "errors" {
  command = plan

  variables {
    name = "lz"
  }

  assert {
    condition     = output.greeting == nonexistent.value
    error_message = "unreachable"
  }
}

run "skipped" {
  command = plan

  variables {
    name = "lz"
  }
}

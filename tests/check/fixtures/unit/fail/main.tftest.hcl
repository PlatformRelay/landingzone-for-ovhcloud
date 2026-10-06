run "greets" {
  command = plan

  variables {
    name = "lz"
  }

  assert {
    condition     = output.greeting == "hello other"
    error_message = "unexpected greeting"
  }
}

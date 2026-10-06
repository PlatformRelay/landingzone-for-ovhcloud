run "greets" {
  command = plan

  variables {
    name = "lz"
  }

  assert {
    condition     = output.greeting == "hello lz"
    error_message = "unexpected greeting"
  }
}

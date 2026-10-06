run "greets" {
  command = plan

  assert {
    condition     = output.greeting ==
    error_message = "unterminated expression"
  }

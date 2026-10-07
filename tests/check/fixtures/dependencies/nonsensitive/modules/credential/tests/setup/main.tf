# Accepted: a helper module of the tests is test configuration.
output "plain" {
  value = nonsensitive(sensitive("x"))
}

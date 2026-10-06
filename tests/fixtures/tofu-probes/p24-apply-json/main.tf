# P24 (005 T007): three provider-free resources. `second` depends on `first`, `third` is
# independent, so the stream shows both the ordered and the parallel case.
resource "terraform_data" "first" {
  input = "p24-first"
}

resource "terraform_data" "second" {
  input = terraform_data.first.id
}

resource "terraform_data" "third" {
  input = "p24-third"
}

resource "terraform_data" "x" {
  input = "x"
  provisioner "local-exec" {
    when    = destroy
    command = "exit 3"
  }
}

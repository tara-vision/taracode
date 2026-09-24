terraform {
  required_providers {
    null = {
      source  = "hashicorp/null"
      version = "~> 3.2"
    }
    local = {
      source  = "hashicorp/local"
      version = "~> 2.5"
    }
  }
}

resource "null_resource" "app" {}

resource "local_file" "config" {
  filename = "${path.module}/app.conf"
  content  = "listen=8080\n"
}

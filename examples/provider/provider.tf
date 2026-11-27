terraform {
  required_providers {
    rcodezero = {
      source = "dg-i/rcodezero"
    }
  }
}

variable "rcodezero_api_token" {
  type      = string
  sensitive = true
}

provider "rcodezero" {
  api_token = var.rcodezero_api_token
  # endpoint = "https://my.rcodezero.at" # default
}

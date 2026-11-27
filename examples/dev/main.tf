terraform {
  required_providers {
    rcodezero = {
      source = "registry.terraform.io/dg-i/rcodezero" # must match dev.tofurc
    }
  }
}

# No default on purpose: forgetting -var-file must fail, not silently hit production.
variable "rcodezero_endpoint" {
  type        = string
  description = "RcodeZero API base URL, e.g. https://my.rcodezero.at"
}

variable "rcodezero_api_token" {
  type      = string
  sensitive = true
}

provider "rcodezero" {
  endpoint  = var.rcodezero_endpoint
  api_token = var.rcodezero_api_token
}

resource "rcodezero_zone" "primary" {
  domain = "example-primary.at"
  type   = "master"

  lifecycle {
    prevent_destroy = false # set to true for real zones
  }
}

resource "rcodezero_zone" "secondary" {
  domain  = "example-secondary.at"
  type    = "slave"
  masters = ["192.0.2.1", "2001:db8::1"]
}

output "nsset" {
  value = rcodezero_zone.primary.nsset
}

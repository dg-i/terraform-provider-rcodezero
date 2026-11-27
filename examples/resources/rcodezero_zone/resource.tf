# Master zone: records are managed at RcodeZero.
resource "rcodezero_zone" "primary" {
  domain = "example.at"
  type   = "master"

  lifecycle {
    # Destroying the zone removes it from public DNS.
    prevent_destroy = true
  }
}

# Slave zone: RcodeZero transfers the zone from your primary nameservers.
resource "rcodezero_zone" "secondary" {
  domain  = "example.com"
  type    = "slave"
  masters = ["192.0.2.1", "2001:db8::1"]
}

output "nsset" {
  description = "Nameservers to delegate the zone to at the parent."
  value       = rcodezero_zone.primary.nsset
}

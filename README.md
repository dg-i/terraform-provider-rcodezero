# Terraform Provider for RcodeZero

[![Test](https://github.com/dg-i/terraform-provider-rcodezero/actions/workflows/test.yml/badge.svg?branch=main)](https://github.com/dg-i/terraform-provider-rcodezero/actions/workflows/test.yml)
[![Latest release](https://img.shields.io/github/v/release/dg-i/terraform-provider-rcodezero?sort=semver)](https://github.com/dg-i/terraform-provider-rcodezero/releases)
[![Terraform Registry](https://img.shields.io/badge/registry-dg--i%2Frcodezero-844FBA?logo=terraform&logoColor=white)](https://registry.terraform.io/providers/dg-i/rcodezero/latest)
[![Go Report Card](https://goreportcard.com/badge/github.com/dg-i/terraform-provider-rcodezero)](https://goreportcard.com/report/github.com/dg-i/terraform-provider-rcodezero)
[![License: MPL 2.0](https://img.shields.io/badge/License-MPL_2.0-brightgreen.svg)](LICENSE)

This Terraform / OpenTofu provider manages zones on the [RcodeZero](https://www.rcodezero.at/) Anycast DNS network via its REST API v2.

## Requirements

- [Terraform](https://developer.hashicorp.com/terraform/downloads) >= 1.0 or [OpenTofu](https://opentofu.org/docs/intro/install/) >= 1.6
- [Go](https://golang.org/doc/install) (see `go.mod` for the version, only for building from source)
- An RcodeZero account with an API token

## Using the Provider

```hcl
terraform {
  required_providers {
    rcodezero = {
      source  = "dg-i/rcodezero"
      version = "~> 0.1"
    }
  }
}

variable "rcodezero_api_token" {
  type      = string
  sensitive = true
}

provider "rcodezero" {
  api_token = var.rcodezero_api_token
}
```

## Provider configuration

The provider reads its configuration only from the provider block, never from environment variables, so what tofu talks to is always visible in the config.

| Attribute | |
|---|---|
| `endpoint` | optional, defaults to `https://my.rcodezero.at` |
| `api_token` | required, sensitive |

Pass the real token as a variable, e.g. `TF_VAR_rcodezero_api_token` in CI, and keep it out of tfvars files in git.

## Resources

| Resource | Description |
|---|---|
| `rcodezero_zone` | A master or slave zone |

### `rcodezero_zone`

| Attribute | |
|---|---|
| `domain` | required, lower case without trailing dot, changing it replaces the zone |
| `type` | `master` or `slave`, changeable in place |
| `masters` | set of IPs, required for `slave`, not allowed for `master` |
| `disabled` | default `false`, master zones only (PATCH `zone_disabled`) |
| `serial_auto_increment` | optional, master zones only, API default `true`; removing it keeps the current value |
| `nsset`, `created`, `id` | computed |

```hcl
resource "rcodezero_zone" "primary" {
  domain = "example.at"
  type   = "master"

  lifecycle {
    prevent_destroy = true # destroying the zone removes it from public DNS
  }
}

resource "rcodezero_zone" "secondary" {
  domain  = "example.com"
  type    = "slave"
  masters = ["192.0.2.1", "2001:db8::1"]
}
```

Import by domain name: `terraform import rcodezero_zone.primary example.at`

See [`docs/`](docs/) and [`examples/`](examples/) for the full reference.

## Known Limitations

- Only zones are managed. RRsets and DNSSEC are not supported yet.

## Development

```sh
make generate   # regenerate API client + mock from api/, and docs/ from the schema
make test       # unit tests, no tofu needed
make testacc    # tofu-driven tests against the in-process mock (TF_ACC=1), uses `tofu` from PATH
make lint       # golangci-lint, same version as in CI
make build      # bin/terraform-provider-rcodezero
```

The API client and the mock server are generated with [oapi-codegen](https://github.com/oapi-codegen/oapi-codegen) from the OpenAPI spec in `api/`; the mock validates every request against that spec. See [CONTRIBUTING.md](CONTRIBUTING.md) for the layout and for running tofu manually against the mock.

## Contributing

Contributions are welcome — see [CONTRIBUTING.md](CONTRIBUTING.md) for
development setup, testing, and PR guidelines. By participating, you agree
to abide by the [Code of Conduct](CODE_OF_CONDUCT.md). Security issues
should be reported privately per [SECURITY.md](SECURITY.md).

## AI-Generated Content

This provider contains AI-generated code. Parts of the source code, tests
and documentation were created with the help of AI coding assistants.

## License

Licensed under the [Mozilla Public License 2.0](LICENSE). Developed and
maintained by [DG-I](https://www.dg-i.net).

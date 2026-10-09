terraform {
  required_version = ">= 1.5"

  required_providers {
    vultr = {
      source  = "vultr/vultr"
      version = "= 2.33.1"
    }
  }
}

# The API key arrives as TF_VAR_vultr_api_key, set by contrib/vultr-boost from
# `mw secrets get`; it is never in a file.
provider "vultr" {
  api_key     = var.vultr_api_key
  rate_limit  = 100
  retry_limit = 3
}

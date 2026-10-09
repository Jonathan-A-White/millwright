# Settings, from the tfvars file in the private vault (hosts/vultr/). None has a
# default that names a person, a host or an address.

variable "region" {
  description = "Vultr region id for the box: the region the hub VPS is in, so the tunnel is short (e.g. ewr, ams; `curl https://api.vultr.com/v2/regions` lists them)."
  type        = string

  validation {
    condition     = can(regex("^[a-z0-9]{2,8}$", var.region))
    error_message = "region must be a Vultr region id such as ewr."
  }
}

variable "plan" {
  description = "Vultr plan id. The default is High Performance, 8 vCPU / 16 GB."
  type        = string
  default     = "vhp-8c-16gb-amd"
}

variable "name" {
  description = "The box's host name in the factory: the host = line of its config.toml and its WireGuard peer name. Letters a-z, digits and hyphens."
  type        = string
  default     = "cloud1"

  validation {
    condition     = can(regex("^[a-z0-9-]+$", var.name))
    error_message = "name must match [a-z0-9-]+ (what wg-enrol accepts)."
  }
}

variable "user" {
  description = "The Linux user the Boost dispatches as (cloud-init makes it, with passwordless sudo)."
  type        = string
  default     = "mw"
}

variable "wg_endpoint" {
  description = "The hub's public WireGuard endpoint, host:port."
  type        = string
}

variable "beads_host" {
  description = "The home's address on WireGuard, where its beads server listens."
  type        = string
}

variable "beads_port" {
  description = "The beads server's port."
  type        = string
  default     = "3307"
}

variable "beads_user" {
  description = "The beads server's user (blank: bd's default)."
  type        = string
  default     = ""
}

variable "rigs" {
  description = "The rigs to clone, as \"name=git-url name=git-url\" (BOOST_RIGS)."
  type        = string
  default     = ""
}

variable "vault_repo" {
  description = "The vault's git url (blank: boost-bootstrap.sh's default)."
  type        = string
  default     = ""
}

variable "cap" {
  description = "How many Builder sessions may run on the box at once (blank: boost-bootstrap.sh's default)."
  type        = string
  default     = ""
}

variable "git_name" {
  description = "The git identity commits are made under."
  type        = string
  default     = ""
}

variable "git_email" {
  description = "The git identity commits are made under."
  type        = string
  default     = ""
}

variable "ssh_authorized_key" {
  description = "A public key that may ssh in as the box's user (blank: nobody can; the box is reached over WireGuard only after you add one)."
  type        = string
  default     = ""
}

variable "ssh_allowed_cidr" {
  description = "The network allowed to reach ssh on the box's public address, as address/prefix."
  type        = string
  default     = "0.0.0.0/0"

  validation {
    condition     = can(cidrhost(var.ssh_allowed_cidr, 0))
    error_message = "ssh_allowed_cidr must be a CIDR such as 0.0.0.0/0."
  }
}

# --- set by contrib/vultr-boost in TF_VAR_* environment variables, never in a file ---

variable "vultr_api_key" {
  description = "The Vultr API key (mw secrets: vultr_api_token)."
  type        = string
  sensitive   = true
}

variable "wg_address" {
  description = "The box's WireGuard address, as the hub assigned it (no mask)."
  type        = string
  default     = ""
}

variable "wg_hub_pubkey" {
  description = "The hub's WireGuard public key."
  type        = string
  default     = ""
}

variable "wg_private_key" {
  description = "The box's WireGuard private key."
  type        = string
  default     = ""
  sensitive   = true
}

variable "github_token" {
  description = "A GitHub token that can read the vault and the rigs (mw secrets: boost_github_token)."
  type        = string
  default     = ""
  sensitive   = true
}

variable "claude_token" {
  description = "A Claude Code token (mw secrets: boost_claude_token)."
  type        = string
  default     = ""
  sensitive   = true
}

variable "beads_password" {
  description = "The beads server's password (mw secrets: boost_beads_password)."
  type        = string
  default     = ""
  sensitive   = true
}

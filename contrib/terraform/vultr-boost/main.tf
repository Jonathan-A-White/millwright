data "vultr_os" "ubuntu" {
  filter {
    name   = "name"
    values = ["Ubuntu 24.04 LTS x64"]
  }
}

locals {
  # boost-bootstrap.sh's inputs, one NAME=VALUE per line; a blank value is left out.
  env_lines = [
    for line in [
      "BOOST_HOST=${var.name}",
      "BOOST_WG_ADDRESS=${var.wg_address}",
      "BOOST_WG_HUB_PUBKEY=${var.wg_hub_pubkey}",
      "BOOST_WG_PRIVATE_KEY=${var.wg_private_key}",
      "BOOST_WG_ENDPOINT=${var.wg_endpoint}",
      "BOOST_BEADS_HOST=${var.beads_host}",
      "BOOST_BEADS_PORT=${var.beads_port}",
      var.beads_user == "" ? "" : "BOOST_BEADS_USER=${var.beads_user}",
      var.beads_password == "" ? "" : "BOOST_BEADS_PASSWORD=${var.beads_password}",
      var.rigs == "" ? "" : "BOOST_RIGS=\"${var.rigs}\"",
      var.vault_repo == "" ? "" : "BOOST_VAULT_REPO=${var.vault_repo}",
      var.cap == "" ? "" : "BOOST_CAP=${var.cap}",
      var.github_token == "" ? "" : "BOOST_GITHUB_TOKEN=${var.github_token}",
      var.claude_token == "" ? "" : "BOOST_CLAUDE_TOKEN=${var.claude_token}",
      var.git_name == "" ? "" : "BOOST_GIT_NAME=\"${var.git_name}\"",
      var.git_email == "" ? "" : "BOOST_GIT_EMAIL=${var.git_email}",
    ] : line if line != ""
  ]

  # Rendered in memory and sent to Vultr; it also lands in the state, which
  # contrib/vultr-boost keeps encrypted. Nothing writes it to a plain file.
  user_data = templatefile("${path.module}/cloud-init.yaml.tftpl", {
    user       = var.user
    ssh_key    = var.ssh_authorized_key
    env_b64    = base64encode("${join("\n", local.env_lines)}\n")
    script_b64 = base64encode(file("${path.module}/../../boost-bootstrap.sh"))
  })

  ssh_cidr_parts = split("/", var.ssh_allowed_cidr)
}

# Only ssh is open to the world; the tunnel to the hub is made from the box outward.
resource "vultr_firewall_group" "boost" {
  description = "${var.name} boost: ssh only"
}

resource "vultr_firewall_rule" "ssh" {
  firewall_group_id = vultr_firewall_group.boost.id
  protocol          = "tcp"
  ip_type           = "v4"
  subnet            = local.ssh_cidr_parts[0]
  subnet_size       = tonumber(local.ssh_cidr_parts[1])
  port              = "22"
  notes             = "ssh"
}

resource "vultr_instance" "boost" {
  label             = var.name
  hostname          = var.name
  region            = var.region
  plan              = var.plan
  os_id             = data.vultr_os.ubuntu.id
  user_data         = local.user_data
  firewall_group_id = vultr_firewall_group.boost.id
  backups           = "disabled"
  enable_ipv6       = false
  activation_email  = false
  tags              = ["millwright", "boost"]
}

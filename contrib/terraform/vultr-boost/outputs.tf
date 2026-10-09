output "id" {
  description = "The Vultr instance id."
  value       = vultr_instance.boost.id
}

output "main_ip" {
  description = "The box's public address."
  value       = vultr_instance.boost.main_ip
}

output "plan" {
  description = "The plan the box was made with."
  value       = vultr_instance.boost.plan
}

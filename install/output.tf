output "vm_container_label" {
  description = "The instance label containing container configuration"
  value       = module.gce-advanced-container.vm_container_label
}

output "container" {
  description = "The container metadata provided to the module"
  value       = module.gce-advanced-container.container
}

output "instance_name" {
  description = "The deployed instance name"
  value       = local.instance_name
}


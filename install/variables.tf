variable "project_id" {
  description = "The project ID to deploy resources into"
  default     = "maintainers-little-helper"
}

variable "subnetwork_project" {
  description = "The project ID where the desired subnetwork is provisioned"
  default     = "maintainers-little-helper"
}

variable "subnetwork" {
  description = "The name of the subnetwork to deploy instances into"
  default     = "default"
}

variable "instance_name" {
  description = "The desired name to assign to the deployed instance"
  default     = "maintainers-vm"
}

variable "zone" {
  description = "The GCP zone to deploy instances into"
  type        = string
  default     = "us-central1-c"
}

variable "region" {
  description = "The GCP region to deploy instances into"
  type        = string
  default     = "us-central1"
}

variable "service_account_name" {
  description = "Service account name (without domain)"
  type        = string
}

variable "container_image" {
  description = "The image name to deploy"
  type        = string
  default     = "quay.io/cilium/github-actions:v1.1.1"
}

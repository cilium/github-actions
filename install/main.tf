provider "google" {
  credentials = file("${path.module}/secrets/account.json")
  project     = var.project_id
  region      = var.region
}

locals {
  instance_name = format("%s-%s", var.instance_name, substr(md5(module.gce-advanced-container.container.image), 0, 8))
}

module "gce-advanced-container" {
  source = "github.com/terraform-google-modules/terraform-google-container-vm"

  container = {
    image = var.container_image
    tty : true
    env = [
      {
        name  = "GITHUB_APP_INTEGRATION_ID"
        value = file("${path.module}/secrets/gh_app_integration_id")
      },
      {
        name  = "GITHUB_OAUTH_CLIENT_ID"
        value = file("${path.module}/secrets/gh_oauth_client_id")
      },
      {
        name  = "GITHUB_OAUTH_CLIENT_SECRET"
        value = file("${path.module}/secrets/gh_oauth_client_secret")
      },
      {
        name  = "GITHUB_APP_WEBHOOK_SECRET"
        value = file("${path.module}/secrets/gh_app_webhook_secret")
      },
      {
        name  = "GITHUB_APP_PRIVATE_KEY"
        value = file("${path.module}/secrets/gh_private.key")
      },
      {
        name  = "LISTEN_PORT"
        value = "80"
      },
      {
        name  = "CONFIG_PATH"
        value = ".github/cilium-actions.yml"
      },
      {
        name  = "LISTEN_ADDRESS"
        value = "0.0.0.0"
      },
      {
        name  = "GITHUB_V3_API_URL"
        value = "https://api.github.com/"
      }
    ]
  }

  restart_policy = "Always"
}

resource "google_compute_instance" "vm" {
  project      = var.project_id
  name         = local.instance_name
  machine_type = "g1-small"
  zone         = var.zone

  boot_disk {
    initialize_params {
      image = module.gce-advanced-container.source_image
    }
  }

  network_interface {
    subnetwork_project = var.subnetwork_project
    subnetwork         = var.subnetwork
    access_config {}
  }

  tags = ["http-server"]

  metadata = {
    gce-container-declaration = module.gce-advanced-container.metadata_value
  }

  labels = {
    container-vm = module.gce-advanced-container.vm_container_label
  }

  service_account {
    email  = format("%s@%s.iam.gserviceaccount.com", var.service_account_name, var.project_id)
    scopes = [
      "https://www.googleapis.com/auth/cloud-platform",
    ]
  }

  lifecycle {
    create_before_destroy = true
  }
}

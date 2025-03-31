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
        name  = "CONFIG_PATHS"
        value = ".github/cilium-actions.yml,.github/maintainers-little-helper.yaml"
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
    # Remove access_config to avoid assigning a public IP
    # access_config {}
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

# Add a Google-managed SSL certificate resource
resource "google_compute_managed_ssl_certificate" "maintainers_lh_cert" {
  name = "maintainers-lh-cert"

  managed {
    domains = ["maintainers-lh.cilium.rocks"]
  }
}

# Add a global forwarding rule and target HTTPS proxy
resource "google_compute_global_forwarding_rule" "https_forwarding_rule" {
  name        = "https-forwarding-rule"
  target      = google_compute_target_https_proxy.https_proxy.self_link
  port_range  = "443"
  ip_protocol = "TCP"
  load_balancing_scheme = "EXTERNAL"
}

# Target HTTPS proxy
resource "google_compute_target_https_proxy" "https_proxy" {
  name             = "https-proxy"
  ssl_certificates = [google_compute_managed_ssl_certificate.maintainers_lh_cert.self_link]
  url_map          = google_compute_url_map.url_map.self_link
}

# URL map to route traffic to the backend service
resource "google_compute_url_map" "url_map" {
  name            = "url-map"
  default_service = google_compute_backend_service.backend_service.self_link
}

# Backend service for the VM instance group
resource "google_compute_backend_service" "backend_service" {
  name                  = "backend-service"
  load_balancing_scheme = "EXTERNAL"
  protocol              = "HTTP"
  backend {
    group = google_compute_instance_group.instance_group.self_link
  }
  health_checks = [google_compute_health_check.http_health_check.self_link]
}

# Instance group for the VM
resource "google_compute_instance_group" "instance_group" {
  name        = "instance-group"
  instances   = [google_compute_instance.vm.self_link]
  zone        = var.zone
}

# HTTP health check for the backend service
resource "google_compute_health_check" "http_health_check" {
  name               = "http-health-check"
  check_interval_sec = 10
  timeout_sec        = 5
  http_health_check {
    port    = 80
    request_path = "/healthz"
  }
}

# Reserve a global static IP for the load balancer
resource "google_compute_global_address" "lb_ip" {
  name = "lb-ip"
}

# Update the global forwarding rule to use the reserved IP
resource "google_compute_global_forwarding_rule" "https_forwarding_rule" {
  name        = "https-forwarding-rule"
  target      = google_compute_target_https_proxy.https_proxy.self_link
  port_range  = "443"
  ip_protocol = "TCP"
  load_balancing_scheme = "EXTERNAL"
  ip_address  = google_compute_global_address.lb_ip.address
}

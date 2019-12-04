# How to install

### Install terraform

1. Download terraform from https://www.terraform.io/downloads.html

### Create service account and access file

```bash
#service_account_name="<username-with-at-least-6-characters>"
#project_name="maintainers-little-helper"

gcloud iam service-accounts create --project="${project_name}" "${service_account_name}"

gcloud projects add-iam-policy-binding "${project_name}" \
    --project "${project_name}" \
    --member "serviceAccount:${service_account_name}@${project_name}.iam.gserviceaccount.com" \
    --role "roles/owner"

gcloud iam service-accounts keys create ./secrets/account.json \
     --iam-account "${service_account_name}@${project_name}.iam.gserviceaccount.com"
```

## Deployment

1. Open https://github.com/organizations/cilium/settings/apps/maintainer-s-little-helper
2. Create and copy each secret / id from that page into a file under secrets/
3. `terraform init`
4. `terraform plan`
5. `terraform apply`
6. `terraform output | grep ipv4 | cut -d = -f2 | xargs echo`
7. Copy the IP from step 6 and paste it under `User authorization callback URL`
   and `Webhook URL` and click `Save changes`
8. Access the VM and check logs. Trigger an event from any open PR by changing
   the labels.
```bash
gcloud compute ssh \
   --project maintainers-little-helper \
   --zone us-central1-c \
   $(terraform output | grep instance_name | cut -d = -f2 | xargs echo -n)
```

Inside the VM
```bash
docker logs -f `docker ps -aq`
```

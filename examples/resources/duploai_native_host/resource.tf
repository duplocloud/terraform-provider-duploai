# A Linux EC2 host with an encrypted gp3 root volume.
resource "duploai_native_host" "linux" {
  workspace_id      = "6a25105705686d697e0da225"
  name              = "my-ec2-host"
  environment_id    = "<environment-id>"
  resource_group_id = "<resource-group-id>"

  subnet_id                 = "subnet-0a727a1199534ecf3"
  os_platform               = 0 # 0 = Linux, 1 = Windows
  disk_size_gb              = 50
  enable_os_disk_encryption = true
  os_disk_encryption_key    = "arn:aws:kms:us-west-2:111122223333:key/d2101b1a-091f-40d8-bb9d-dd3c95be326e"
  metadata_service_option   = "V2Only"

  # AWS RunInstances request, flattened.
  instance_type = "t3a.small"
  ebs_optimized = true

  block_device_mappings = [
    {
      device_name = "/dev/xvda"
      ebs = {
        volume_size           = 50
        volume_type           = "gp3"
        delete_on_termination = true
        encrypted             = true
        kms_key_id            = "arn:aws:kms:us-west-2:111122223333:key/d2101b1a-091f-40d8-bb9d-dd3c95be326e"
      }
    }
  ]
}

output "host_instance_id" {
  value = duploai_native_host.linux.instance_id
}

output "host_private_ip" {
  value = duploai_native_host.linux.private_ip_address
}

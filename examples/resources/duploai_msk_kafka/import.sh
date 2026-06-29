# Import an existing provisioned MSK cluster.
#  - WORKSPACE_ID is the workspace the cluster belongs to
#  - MSK_ID is the unique identifier of the cluster (e.g. 6a2258e94703bc957a1b824e)
terraform import duploai_msk_kafka.example WORKSPACE_ID/MSK_ID
# Example:
# terraform import duploai_msk_kafka.example 6a25105705686d697e0da225/6a2258e94703bc957a1b824e

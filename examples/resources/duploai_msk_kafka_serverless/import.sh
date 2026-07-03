# Import an existing serverless MSK cluster.
#  - WORKSPACE_ID is the workspace the cluster belongs to
#  - MSK_ID is the unique identifier of the cluster (e.g. 6a2258e94703bc957a1b824e)
terraform import duploai_msk_kafka_serverless.example WORKSPACE_ID/MSK_ID
# Example:
# terraform import duploai_msk_kafka_serverless.example 6a25105705686d697e0da225/6a2258e94703bc957a1b824e

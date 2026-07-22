# Import an existing SQS queue.
#  - WORKSPACE_ID is the workspace the queue belongs to
#  - SQS_ID is the unique identifier of the queue (e.g. 6a2258e94703bc957a1b824e)
terraform import duploai_sqs.orders WORKSPACE_ID/SQS_ID
# Example:
# terraform import duploai_sqs.orders 6a25105705686d697e0da225/6a2258e94703bc957a1b824e

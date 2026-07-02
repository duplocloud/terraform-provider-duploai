# Import an existing SNS topic.
#  - WORKSPACE_ID is the workspace the topic belongs to
#  - SNS_ID is the unique identifier of the topic (e.g. 6a2258e94703bc957a1b824e)
terraform import duploai_sns.example WORKSPACE_ID/SNS_ID
# Example:
# terraform import duploai_sns.example 6a25105705686d697e0da225/6a2258e94703bc957a1b824e

# Import an existing ECR repository.
#  - WORKSPACE_ID is the workspace the repository belongs to
#  - ECR_ID is the unique identifier of the repository (e.g. 6a2258e94703bc957a1b824e)
terraform import duploai_ecr.backend WORKSPACE_ID/ECR_ID
# Example:
# terraform import duploai_ecr.backend 6a25105705686d697e0da225/6a2258e94703bc957a1b824e

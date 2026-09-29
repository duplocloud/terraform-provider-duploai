# Import an existing Git repository resource.
#  - WORKSPACE_ID is the unique identifier of the workspace (e.g. 69b2aa30675718845bfe87a0)
#  - GIT_REPOSITORY_ID is the unique identifier of the Git repository (e.g. 6a2258e94703bc957a1b824e)
terraform import duploai_git_repository.podinfo WORKSPACE_ID/GIT_REPOSITORY_ID
# Example:
# terraform import duploai_git_repository.podinfo 69b2aa30675718845bfe87a0/6a2258e94703bc957a1b824e

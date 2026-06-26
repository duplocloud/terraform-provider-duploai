# Import an existing native host.
#  - WORKSPACE_ID is the workspace the host belongs to
#  - HOST_ID is the unique identifier of the host (e.g. 6a2258e94703bc957a1b824e)
terraform import duploai_native_host.myhost WORKSPACE_ID/HOST_ID
# Example:
# terraform import duploai_native_host.myhost 6a25105705686d697e0da225/6a2258e94703bc957a1b824e

# Import an existing plan resource.
#  - WORKSPACE_ID is the unique identifier of the workspace (e.g. 69b2aa30675718845bfe87a0)
#  - PLAN_ID is the unique identifier of the plan (e.g. 6a2258e94703bc957a1b824e)
terraform import duploai_plan.myplan WORKSPACE_ID/PLAN_ID
# Example:
# terraform import duploai_plan.myplan 69b2aa30675718845bfe87a0/6a2258e94703bc957a1b824e

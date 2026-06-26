# Custom skill defined in Markdown. Keep the content in its own file and read it
# with the built-in file() function (use templatefile() if you need variables).
resource "duploai_admin_skill" "markdown" {
  name     = "incident-runbook"
  type     = "Custom"
  format   = "SkillMd"
  skill_md = file("path/to/skill.md")
}

# Custom skill loaded from a private Git repository.
resource "duploai_admin_skill" "git" {
  name   = "infra-skills"
  type   = "Custom"
  format = "PrivateGitRepo"

  git_repo = {
    name        = "skills-repo"
    org_name    = "duplocloud"
    branch      = "main"
    folder_path = "skills/infra"
    scope_id    = "<scope-id>"
  }
}

# External skill delivered as a package.
resource "duploai_admin_skill" "external" {
  name         = "vendor-skill"
  type         = "External"
  vendor       = "acme"
  package_path = "packages/acme-skill.zip"
}

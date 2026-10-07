// Terramate project root (005 T038; ADR-0007, research R16). The landing-zone generation
// configuration lives in stacks/_lz/ and is imported by stacks/lz.tm.hcl, so it applies to the
// stacks below stacks/ only.
//
// required_version is what makes this directory the project root where there is no git
// repository (P17, T007): stacks:check and the generation controls run on a scratch copy.
terramate {
  required_version = "0.17.3"
  config {
    git {
      default_branch = "main"
    }
  }
}

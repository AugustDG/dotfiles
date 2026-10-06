package bootstrap

var GlobalBrewPackages = []string{
	"fzf",
	"atuin",
	"jandedobbeleer/oh-my-posh/oh-my-posh",
	"oven-sh/bun/bun",
	"mise",
	"pnpm",
	"node",
	"ripgrep",
	"fd",
	"jq",
}

var CoreBrewPackages = []string{
	"zsh",
	"git",
	"gh",
	"stow",
}

const (
	HopperRepo = "AugustDG/hopper"
	GhottoRepo = "AugustDG/ghotto" // provides the `gho` binary
	ZnapURL    = "https://github.com/marlonrichert/zsh-snap.git"
	ZnapDir    = ".plugins/znap"
)

var BackupTargets = []string{
	".zshrc",
	".zshenv",
	".zprofile",
	".gitconfig",
}

var ZprofileLocalTemplate = `# Machine-local exports and secrets. Not tracked by dotfiles.
# Sourced from .zprofile, once per login shell.

# export CLOUD_API_ENDPOINT=https://api.botpress.cloud
# export CLOUD_PAT=bp_pat_xxxxxxxxxxxxxxxx
# export CLOUD_BOT_ID=xxxxxxxxxxxxxxxxxxx
`

var ZshrcLocalTemplate = `# Machine-local interactive setup (aliases, functions). Not tracked by dotfiles.
# Sourced from .zshrc; exports and secrets go in ~/.zprofile.local.
`

package bootstrap

var GlobalBrewPackages = []string{
	"fzf",
	"atuin",
	"jandedobbeleer/oh-my-posh/oh-my-posh",
	"oven-sh/bun/bun",
	"nvm",
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

var ZshrcLocalTemplate = `# Machine-local overrides. Not tracked by dotfiles.
# Fill in whichever are relevant to this machine.

# export CLOUD_API_ENDPOINT=https://api.botpress.cloud
# export CLOUD_PAT=bp_pat_xxxxxxxxxxxxxxxx
# export CLOUD_BOT_ID=xxxxxxxxxxxxxxxxxxx
`

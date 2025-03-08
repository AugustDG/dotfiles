#!/bin/bash

echo "Changing directory to $HOME"
cd $HOME

echo "Installing git, tar, wget & other essentials..."
sudo apt-get update 
sudo apt-get -y install tar unzip wget curl git build-essential

echo "Installing nvim from GitHub at /opt/nvim..."
curl -LO https://github.com/neovim/neovim/releases/latest/download/nvim-linux-x86_64.tar.gz 
sudo rm -rf /opt/nvim
sudo tar -C /opt -xzf nvim-linux-x86_64.tar.gz 
nvim-linux-x86_64.tar.gz 

echo "Installing pyenv (and dependencies) with default python 3.10..." 
sudo apt-get -y install libssl-dev zlib1g-dev libbz2-dev libreadline-dev libsqlite3-dev curl\
    libncursesw5-dev xz-utils tk-dev libxml2-dev libxmlsec1-dev libffi-dev liblzma-dev
curl -fsSL https://pyenv.run | bash

.pyenv/bin/pyenv install 3.10
.pyenv/bin/pyenv global 3.10

if gh --version ; then
    echo "Skipping GitHub CLI installation because it's already installed!"
else
    echo "Installing GitHub CLI..."
    sudo mkdir -p -m 755 /etc/apt/keyrings \
    && wget -qO- https://cli.github.com/packages/githubcli-archive-keyring.gpg | sudo tee /etc/apt/keyrings/githubcli-archive-keyring.gpg \
    && sudo chmod go+r /etc/apt/keyrings/githubcli-archive-keyring.gpg \
    && echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" | sudo tee /etc/apt/sources.list.d/github-cli.list  \
    && sudo apt-get update \
    && sudo apt-get install gh -y 
fi

# Check if there's an authenticated user in GitHub CLI
if gh auth status ; then
    echo "Skipping GitHub CLI auth because it's already done!"
else
    echo "Authenticating GitHub CLI..."
    gh auth login -p ssh -h github.com -w
fi

echo "Setting up git credentials from GitHub CLI..."
gh auth setup-git

echo "Installing xsel..."
sudo apt-get install xsel -y

echo "Installing tmux..."
sudo apt-get install tmux -y

echo "Installing rofi..."
sudo apt-get install rofi -y

echo "Installing fzf from GitHub at $HOME/.fzf..."
git clone --depth 1 https://github.com/junegunn/fzf.git $HOME/.fzf 
$HOME/.fzf/install --key-bindings --completion --no-update-rc 

echo "Installing oh-my-posh from install script at $HOME/bin/oh-my-posh..."
mkdir -p $HOME/bin
curl -s https://ohmyposh.dev/install.sh | bash -s -- -d $HOME/bin 

echo "Downloading & applying personal dotfiles in $HOME..."
git clone https://github.com/AugustDG/dotfiles.git $HOME/.dot --bare

function git-dot {
  /usr/bin/git --git-dir=$HOME/.dot/ --work-tree=$HOME $@
}

mkdir -p .dot-backup
git-dot checkout

if [ $? = 0 ]; then
    echo "  Checked out dotfiles!";
else
    echo "  Backing up pre-existing dotfiles...";
    git-dot checkout 2>&1 | egrep "\s+\." | awk {'print $1'} | xargs -I{} mv {} .dot-backup/{}
fi;

git-dot checkout
git-dot submodule update --init --recursive
git-dot config status.showUntrackedFiles no

echo "Autoremoving packages..."
sudo apt-get autoremove -y 

echo "Make sure to restart your shell to see all the changes!"

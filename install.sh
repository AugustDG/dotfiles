#!/bin/bash

echo "Installing git, tar, wget & other essentials..."
sudo apt update >/dev/null 2>&1
sudo apt install tar unzip wget curl git build-essential >/dev/null 2>&1

echo "Installing nvim from GitHub at /opt/nvim..."
curl -LO https://github.com/neovim/neovim/releases/latest/download/nvim-linux64.tar.gz >/dev/null
sudo rm -rf /opt/nvim
sudo tar -C /opt -xzf nvim-linux64.tar.gz >/dev/null 2>&1
rm nvim-linux64.tar.gz

echo "Installing Miniconda at $HOME/miniconda3..." 
mkdir -p $HOME/miniconda3
wget https://repo.anaconda.com/miniconda/Miniconda3-latest-Linux-x86_64.sh -O $HOME/miniconda3/miniconda.sh >/dev/null 2>&1
bash $HOME/miniconda3/miniconda.sh -b -u -p $HOME/miniconda3 >/dev/null 2>&1
rm $HOME/miniconda3/miniconda.sh

echo "Installing GitHub CLI..."
(type -p wget >/dev/null || (sudo apt update && sudo apt-get install wget -y)) \
&& sudo mkdir -p -m 755 /etc/apt/keyrings \
&& wget -qO- https://cli.github.com/packages/githubcli-archive-keyring.gpg | sudo tee /etc/apt/keyrings/githubcli-archive-keyring.gpg >/dev/null 2>&1\
&& sudo chmod go+r /etc/apt/keyrings/githubcli-archive-keyring.gpg \
&& echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" | sudo tee /etc/apt/sources.list.d/github-cli.list >/dev/null 2>&1 \
&& sudo apt update >/dev/null 2>&1\
&& sudo apt install gh -y >/dev/null 2>&1

# Check if there's an authenticated user in GitHub CLI
if gh auth status >/dev/null 2>&1; then
    echo "Skipping GitHub CLI auth because it's already done!"
else
    echo "Authenticating GitHub CLI..."
    gh auth login -p ssh -h github.com -w
fi

echo "Setting up git credentials from GitHub CLI..."
gh auth setup-git

echo "Installing tmux..."
sudo apt install tmux >/dev/null 2>&1

echo "Installing fzf from GitHub at $HOME/.fzf..."
git clone --depth 1 https://github.com/junegunn/fzf.git $HOME/.fzf >/dev/null 2>&1
$HOME/.fzf/install --key-bindings --completion --no-update-rc >/dev/null 2>&1

echo "Installing oh-my-posh from install script at $HOME/bin/oh-my-posh..."
mkdir -p $HOME/bin
curl -s https://ohmyposh.dev/install.sh | bash -s -- -d $HOME/bin >/dev/null 2>&1

echo "Downloading & applying personal dotfiles in $HOME..."
git clone https://github.com/AugustDG/dotfiles.git $HOME/dotfiles --recursive >/dev/null 2>&1
sudo chown -R $USER $HOME/dotfiles
sudo cp -r $HOME/dotfiles/. $HOME
sudo rm -r $HOME/dotfiles

echo "Autoremoving packages..."
sudo apt autoremove -y >/dev/null 2>&1

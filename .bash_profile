# ~/.bash_profile

if [[ -f ~/.bashrc ]]; then
  . ~/.bashrc
fi

# if [ -x "$HOME/easytv/easytv-audio-init.sh" ]; then
#   "$HOME"/easytv/easytv-init-audio.sh
# fi

if [[ -z "$DISPLAY" && "$(tty)" == "/dev/tty1" ]]; then
  exec startx
fi

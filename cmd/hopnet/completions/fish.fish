# fish completion for hopnet. Source via `hopnet completion fish | source`.
function __fish_hopnet_complete
    set -l cmd (commandline -opc)
    set -l cur (commandline -ct)
    if test -n "$cur"; and string match -q -- '-*' "$cur"
        $cmd "$cur" --generate-bash-completion
    else
        $cmd --generate-bash-completion
    end
end
complete -c hopnet --no-files -a "(__fish_hopnet_complete)"

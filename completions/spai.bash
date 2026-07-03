# bash completion for spai
# Source this file or drop it in ~/.local/share/bash-completion/completions/spai

_spai() {
    local cur prev words cword
    _init_completion 2>/dev/null || {
        COMPREPLY=()
        cur="${COMP_WORDS[COMP_CWORD]}"
        prev="${COMP_WORDS[COMP_CWORD-1]}"
        words=("${COMP_WORDS[@]}")
        cword=$COMP_CWORD
    }

    local subcommands="init setup llm clear compact rebuild-context history sessions resume"
    local global_flags="--dry-run --local --verbose --autonomous --legal --version --session"

    if [[ $cword -eq 1 ]]; then
        COMPREPLY=($(compgen -W "$subcommands $global_flags" -- "$cur"))
        return
    fi

    case "${words[1]}" in
        llm)
            if [[ $cword -eq 2 ]]; then
                local llm_cmds="status install use-runtime list pull remove use"
                COMPREPLY=($(compgen -W "$llm_cmds" -- "$cur"))
            fi
            ;;
        clear)
            COMPREPLY=($(compgen -W "--lines --session" -- "$cur"))
            ;;
        compact|rebuild-context|history)
            COMPREPLY=($(compgen -W "--session" -- "$cur"))
            ;;
        sessions)
            if [[ $cword -eq 2 ]]; then
                COMPREPLY=($(compgen -W "--reset" -- "$cur"))
            fi
            ;;
    esac
}

complete -F _spai spai

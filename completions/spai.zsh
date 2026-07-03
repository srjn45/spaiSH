#compdef spai
# zsh completion for spai
# Drop this file as _spai in a directory on your $fpath

_spai() {
    local context state state_descr line
    typeset -A opt_args

    _arguments -C \
        '1: :->subcommand' \
        '*:: :->args' && return 0

    case $state in
        subcommand)
            local -a subcommands
            subcommands=(
                'init:configure your AI provider'
                'setup:configure your AI provider (alias for init)'
                'llm:manage local LLM runtimes and models'
                'clear:wipe session or keep latest N messages'
                'compact:AI-summarise session history'
                'rebuild-context:rebuild AI context from history'
                'history:browse session history in pager'
                'sessions:list or switch sessions'
                'resume:resume the most recent session'
            )
            _describe 'subcommand' subcommands
            ;;
        args)
            case $line[1] in
                llm)
                    local -a llm_cmds
                    llm_cmds=(
                        'status:show runtime and model status'
                        'install:install a runtime (ollama or bitnet; default: ollama)'
                        'use-runtime:switch active runtime (ollama, bitnet)'
                        'list:list installed and recommended models'
                        'pull:download a model (e.g. qwen2.5-coder:7b)'
                        'remove:delete a model from local storage'
                        'use:set the active model for local inference'
                    )
                    _describe 'llm command' llm_cmds
                    ;;
                clear)
                    _arguments \
                        '--lines[keep only the last N messages]:N' \
                        '--session[named session]:session ID'
                    ;;
                compact|rebuild-context|history)
                    _arguments \
                        '--session[named session]:session ID'
                    ;;
                sessions)
                    _arguments \
                        '--reset[clear pinned session]' \
                        ':session ID'
                    ;;
                *)
                    _arguments \
                        '--dry-run[show plan without executing]' \
                        '--local[force local model]' \
                        '--verbose[show full command output and iteration details]' \
                        '--autonomous[run all commands without confirmation prompts]' \
                        '--legal[print legal disclaimer and exit]' \
                        '--version[print version and exit]' \
                        '--session[named session]:session ID' \
                        '*:query'
                    ;;
            esac
            ;;
    esac
}

_spai "$@"

# fish completion for spai
# Drop this file in ~/.config/fish/completions/spai.fish

set -l subcommands init setup llm clear compact rebuild-context history sessions resume

# Subcommands (offered when no subcommand has been typed yet)
complete -c spai -f -n "not __fish_seen_subcommand_from $subcommands" -a init            -d 'Configure your AI provider'
complete -c spai -f -n "not __fish_seen_subcommand_from $subcommands" -a setup           -d 'Configure your AI provider (alias for init)'
complete -c spai -f -n "not __fish_seen_subcommand_from $subcommands" -a llm             -d 'Manage local LLM runtimes and models'
complete -c spai -f -n "not __fish_seen_subcommand_from $subcommands" -a clear           -d 'Wipe session or keep latest N messages'
complete -c spai -f -n "not __fish_seen_subcommand_from $subcommands" -a compact         -d 'AI-summarise session history'
complete -c spai -f -n "not __fish_seen_subcommand_from $subcommands" -a rebuild-context -d 'Rebuild AI context from history'
complete -c spai -f -n "not __fish_seen_subcommand_from $subcommands" -a history         -d 'Browse session history in pager'
complete -c spai -f -n "not __fish_seen_subcommand_from $subcommands" -a sessions        -d 'List or switch sessions'
complete -c spai -f -n "not __fish_seen_subcommand_from $subcommands" -a resume          -d 'Resume the most recent session'

# Global flags (when no subcommand)
complete -c spai -n "not __fish_seen_subcommand_from $subcommands" -l dry-run    -d 'Show plan without executing'
complete -c spai -n "not __fish_seen_subcommand_from $subcommands" -l local      -d 'Force local model'
complete -c spai -n "not __fish_seen_subcommand_from $subcommands" -l verbose    -d 'Show full command output and iteration details'
complete -c spai -n "not __fish_seen_subcommand_from $subcommands" -l autonomous -d 'Run all commands without confirmation prompts'
complete -c spai -n "not __fish_seen_subcommand_from $subcommands" -l legal      -d 'Print legal disclaimer and exit'
complete -c spai -n "not __fish_seen_subcommand_from $subcommands" -l version    -d 'Print version and exit'
complete -c spai -n "not __fish_seen_subcommand_from $subcommands" -l session -r -d 'Named session'

# llm subcommands
set -l llm_cmds status install use-runtime list pull remove use
complete -c spai -f -n "__fish_seen_subcommand_from llm; and not __fish_seen_subcommand_from $llm_cmds" -a status      -d 'Show runtime and model status'
complete -c spai -f -n "__fish_seen_subcommand_from llm; and not __fish_seen_subcommand_from $llm_cmds" -a install     -d 'Install a runtime (ollama or bitnet; default: ollama)'
complete -c spai -f -n "__fish_seen_subcommand_from llm; and not __fish_seen_subcommand_from $llm_cmds" -a use-runtime -d 'Switch active runtime (ollama, bitnet)'
complete -c spai -f -n "__fish_seen_subcommand_from llm; and not __fish_seen_subcommand_from $llm_cmds" -a list        -d 'List installed and recommended models'
complete -c spai -f -n "__fish_seen_subcommand_from llm; and not __fish_seen_subcommand_from $llm_cmds" -a pull        -d 'Download a model (e.g. qwen2.5-coder:7b)'
complete -c spai -f -n "__fish_seen_subcommand_from llm; and not __fish_seen_subcommand_from $llm_cmds" -a remove      -d 'Delete a model from local storage'
complete -c spai -f -n "__fish_seen_subcommand_from llm; and not __fish_seen_subcommand_from $llm_cmds" -a use         -d 'Set the active model for local inference'

# clear flags
complete -c spai -n "__fish_seen_subcommand_from clear" -l lines   -r -d 'Keep only the last N messages'
complete -c spai -n "__fish_seen_subcommand_from clear" -l session -r -d 'Named session'

# compact / rebuild-context / history flags
complete -c spai -n "__fish_seen_subcommand_from compact rebuild-context history" -l session -r -d 'Named session'

# sessions flags
complete -c spai -n "__fish_seen_subcommand_from sessions" -l reset -d 'Clear pinned session'

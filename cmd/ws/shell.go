package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/d1ys3nk0/ws/internal/ws"
)

func withinPath(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && (rel == "." || rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}
func navigate(path string) error {
	destination := os.Getenv("WS_CD_FILE")
	if destination == "" {
		fmt.Fprintln(os.Stderr, "Shell navigation: enable with eval \"$(ws shell init bash)\" (or zsh).")
		return nil
	}
	f, err := os.OpenFile(destination, os.O_WRONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || stat.Nlink != 1 {
		return fmt.Errorf("unsafe shell navigation file")
	}
	if err = f.Truncate(0); err != nil {
		return err
	}
	_, err = f.WriteString(path + "\n")
	return err
}
func printShell(shell string) error {
	if shell != "bash" && shell != "zsh" {
		return fmt.Errorf("supported shells: bash, zsh")
	}
	fmt.Fprint(os.Stdout, `ws() {
    local ws_cd_file ws_exit_code=0 ws_destination
    ws_cd_file=$(mktemp "${TMPDIR:-/tmp}/ws-cd.XXXXXXXX") || return
    WS_CD_FILE="$ws_cd_file" command ws "$@" || ws_exit_code=$?
    if [ -s "$ws_cd_file" ]; then
        IFS= read -r ws_destination < "$ws_cd_file"
        cd -- "$ws_destination" || { [ "$ws_exit_code" -ne 0 ] || ws_exit_code=1; }
    fi
    rm -f -- "$ws_cd_file"
    return "$ws_exit_code"
}
`)
	if shell == "bash" {
		fmt.Fprint(os.Stdout, `_ws_complete() {
    local ws_word="${COMP_WORDS[COMP_CWORD]}" ws_previous="${COMP_WORDS[COMP_CWORD-1]}" ws_workspace="" ws_i ws_kind="changes" ws_prefix="" ws_item ws_command=""
    local -a ws_context=()
    for ((ws_i=1; ws_i<COMP_CWORD; ws_i++)); do
        case "${COMP_WORDS[ws_i]}" in
            --workspace) ws_workspace="${COMP_WORDS[ws_i+1]}"; ((ws_i++)) ;;
            --color) ((ws_i++)) ;;
            --workspace=*) ws_workspace="${COMP_WORDS[ws_i]#*=}" ;;
            -*) ;;
            *) [[ -z "$ws_command" ]] && ws_command="${COMP_WORDS[ws_i]}" ;;
        esac
    done
    [[ -n "$ws_workspace" ]] && ws_context=(--workspace "$ws_workspace")
    if [[ "$ws_previous" == --only || "$ws_previous" == --except || "$ws_word" == --only=* || "$ws_word" == --except=* ]]; then
        ws_kind=repositories
        [[ "$ws_command" == diff ]] && ws_kind=diff-repositories
        [[ "$ws_word" == *=* ]] && ws_prefix="${ws_word%%=*}=" && ws_word="${ws_word#*=}"
        [[ "$ws_word" == *,* ]] && ws_prefix="$ws_prefix${ws_word%,*}," && ws_word="${ws_word##*,}"
    elif [[ "$ws_word" == -* ]]; then
        ws_kind="flags:$ws_command"
    elif [[ -z "$ws_command" ]]; then ws_kind=commands
    fi
    if [[ "$ws_command" == restore && "$ws_kind" == changes ]]; then COMPREPLY=( $(compgen -f -- "$ws_word") ); return; fi
    COMPREPLY=()
    while IFS= read -r ws_item; do
        [[ "$ws_item" == "$ws_word"* ]] && COMPREPLY+=("$ws_prefix$ws_item")
    done < <(command ws "${ws_context[@]}" _complete "$ws_kind" 2>/dev/null)
}
complete -F _ws_complete ws
`)
	} else {
		fmt.Fprint(os.Stdout, `_ws_complete() {
    local ws_word="${words[CURRENT]}" ws_previous="${words[CURRENT-1]}" ws_workspace="" ws_i ws_kind=changes ws_prefix="" ws_command=""
    local -a ws_values ws_context
    for ((ws_i=2; ws_i<CURRENT; ws_i++)); do
        case "${words[ws_i]}" in
            --workspace) ws_workspace="${words[ws_i+1]}"; ((ws_i++)) ;;
            --color) ((ws_i++)) ;;
            --workspace=*) ws_workspace="${words[ws_i]#*=}" ;;
            -*) ;;
            *) [[ -z "$ws_command" ]] && ws_command="${words[ws_i]}" ;;
        esac
    done
    [[ -n "$ws_workspace" ]] && ws_context=(--workspace "$ws_workspace")
    if [[ "$ws_previous" == --only || "$ws_previous" == --except || "$ws_word" == --only=* || "$ws_word" == --except=* ]]; then
        ws_kind=repositories
        [[ "$ws_command" == diff ]] && ws_kind=diff-repositories
        [[ "$ws_word" == *=* ]] && ws_prefix="${ws_word%%=*}=" && ws_word="${ws_word#*=}"
        [[ "$ws_word" == *,* ]] && ws_prefix="$ws_prefix${ws_word%,*},"
    elif [[ "$ws_word" == -* ]]; then ws_kind="flags:$ws_command"
    elif [[ -z "$ws_command" ]]; then ws_kind=commands
    fi
    if [[ "$ws_command" == restore && "$ws_kind" == changes ]]; then _files; return; fi
    ws_values=("${(@f)$(command ws "${ws_context[@]}" _complete "$ws_kind" 2>/dev/null)}")
    [[ -n "$ws_prefix" ]] && compset -P "$ws_prefix"
    compadd -- "${ws_values[@]}"
}
if (( $+functions[compdef] )); then compdef _ws_complete ws; fi
`)
	}
	return nil
}
func complete(engine *ws.Engine, kind string) error {
	switch {
	case kind == "commands":
		for _, c := range commands {
			if c.description != "" {
				fmt.Fprintln(os.Stdout, c.name)
			}
		}
	case strings.HasPrefix(kind, "flags:"):
		if kind == "flags:" {
			for _, f := range []string{"workspace", "json", "color", "help"} {
				fmt.Fprintln(os.Stdout, "--"+f)
			}
			return nil
		}
		for _, c := range commands {
			if c.name == strings.TrimPrefix(kind, "flags:") {
				for _, f := range append([]string{"workspace", "json", "color", "help"}, c.options...) {
					if f == "f" {
						fmt.Fprintln(os.Stdout, "-f")
					} else {
						fmt.Fprintln(os.Stdout, "--"+f)
					}
				}
			}
		}
	case kind == "repositories" || kind == "diff-repositories":
		if kind == "diff-repositories" {
			fmt.Fprintln(os.Stdout, "root")
		}
		for _, r := range engine.Config.Children {
			fmt.Fprintln(os.Stdout, r.Name)
		}
	default:
		all, err := engine.All()
		if err != nil {
			return err
		}
		for _, m := range all {
			if m.Version == 5 {
				fmt.Fprintln(os.Stdout, m.WorkspaceID)
			} else {
				fmt.Fprintln(os.Stdout, m.Tag)
			}
		}
	}
	return nil
}
